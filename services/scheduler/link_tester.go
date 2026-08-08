package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sublink/constants"
	"sublink/models"
	"sublink/services/mihomo"
	"sublink/utils"
)

// LinkTestMode 通用链路测试模式
type LinkTestMode string

const (
	// LinkTestModeDelay 仅测延时
	LinkTestModeDelay LinkTestMode = "delay"
	// LinkTestModeSpeed 仅测速度（必要时会附带延时结果）
	LinkTestModeSpeed LinkTestMode = "speed"
	// LinkTestModeBoth 先延时后测速（抓取入库场景，仅成功回调）
	LinkTestModeBoth LinkTestMode = "both"
	// LinkTestModeFull 全量检测：先延时再按策略测速，每条都回调
	LinkTestModeFull LinkTestMode = "full"
)

// LinkTestItem 待测链路条目（与主节点表解耦）
type LinkTestItem struct {
	ID              int
	Link            string
	Name            string
	PrevDelayTime   int
	PrevDelayStatus string
	PrevSpeed       float64
	PrevSpeedStatus string
	PrevIsValid     bool
}

// LinkTestResult 单条链路测试结果
type LinkTestResult struct {
	ID          int
	Link        string
	Name        string
	DelayTime   int
	DelayStatus string
	Speed       float64
	SpeedStatus string
	IsValid     bool
	Err         error
}

// LinkTestSummary 汇总结果
type LinkTestSummary struct {
	Total   int
	Success int
	Failed  int
}

// DefaultSpeedTestConfig 返回与节点检测一致的默认测速配置
func DefaultSpeedTestConfig() *SpeedTestConfig {
	return &SpeedTestConfig{
		SpeedTestURL:       "https://speed.cloudflare.com/__down?bytes=10000000",
		LatencyTestURL:     "http://cp.cloudflare.com/generate_204",
		Timeout:            8 * time.Second,
		Mode:               "mihomo",
		LatencyConcurrency: 20,
		SpeedConcurrency:   5,
		IncludeHandshake:   true,
		SpeedRecordMode:    "average",
		PeakSampleInterval: 100,
	}
}

// ResolveSpeedTestConfig 解析测速配置：
// profileID>0 使用指定策略；否则优先使用第一个节点检测策略；都没有则用默认值。
func ResolveSpeedTestConfig(profileID int) (*SpeedTestConfig, string) {
	if profileID > 0 {
		if profile, err := models.GetNodeCheckProfileByID(profileID); err == nil && profile != nil {
			return SpeedTestConfigFromProfile(profile), profile.Name
		}
	}

	var p models.NodeCheckProfile
	if profiles, err := p.List(); err == nil && len(profiles) > 0 {
		return SpeedTestConfigFromProfile(&profiles[0]), profiles[0].Name
	}
	return DefaultSpeedTestConfig(), "default"
}

func normalizeLinkTestConfig(config *SpeedTestConfig) *SpeedTestConfig {
	if config == nil {
		config = DefaultSpeedTestConfig()
	}
	cfg := *config
	if strings.TrimSpace(cfg.SpeedTestURL) == "" {
		cfg.SpeedTestURL = "https://speed.cloudflare.com/__down?bytes=10000000"
	}
	if strings.TrimSpace(cfg.LatencyTestURL) == "" {
		cfg.LatencyTestURL = cfg.SpeedTestURL
		if strings.TrimSpace(cfg.LatencyTestURL) == "" {
			cfg.LatencyTestURL = "http://cp.cloudflare.com/generate_204"
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	if cfg.Mode == "" {
		cfg.Mode = "mihomo"
	}
	if cfg.SpeedRecordMode == "" {
		cfg.SpeedRecordMode = "average"
	}
	if cfg.PeakSampleInterval <= 0 {
		cfg.PeakSampleInterval = 100
	}
	if cfg.SpeedConcurrency < 0 {
		cfg.SpeedConcurrency = 0
	}
	if cfg.LatencyConcurrency < 0 {
		cfg.LatencyConcurrency = 0
	}
	return &cfg
}

func resolveFixedConcurrency(configured, fallback, max int) int {
	if configured <= 0 {
		if fallback <= 0 {
			fallback = 5
		}
		return fallback
	}
	if max > 0 && configured > max {
		return max
	}
	return configured
}

// RunLinkTests 使用通用测速配置对任意链路做并发延时/速度测试。
// 不写主节点表，结果通过 onResult 回调（可写 GitHub 独立节点等）。
func RunLinkTests(ctx context.Context, items []LinkTestItem, config *SpeedTestConfig, mode LinkTestMode, onResult func(LinkTestResult)) LinkTestSummary {
	if len(items) == 0 {
		return LinkTestSummary{}
	}
	if mode == "" {
		mode = LinkTestModeDelay
	}
	cfg := normalizeLinkTestConfig(config)
	if ctx == nil {
		ctx = context.Background()
	}

	utils.Info("通用链路测试开始: total=%d mode=%s latencyConc=%d speedConc=%d timeout=%s",
		len(items), mode, cfg.LatencyConcurrency, cfg.SpeedConcurrency, cfg.Timeout)

	switch mode {
	case LinkTestModeSpeed:
		return runLinkSpeedOnly(ctx, items, cfg, onResult)
	case LinkTestModeBoth:
		return runLinkBoth(ctx, items, cfg, onResult)
	case LinkTestModeFull:
		return runLinkFull(ctx, items, cfg, onResult)
	default:
		return runLinkDelayOnly(ctx, items, cfg, onResult)
	}
}

func emitResult(onResult func(LinkTestResult), result LinkTestResult, ok bool, success *int32, failed *int32) {
	if ok {
		atomic.AddInt32(success, 1)
	} else {
		atomic.AddInt32(failed, 1)
	}
	if onResult != nil {
		onResult(result)
	}
}

func runLinkDelayOnly(ctx context.Context, items []LinkTestItem, cfg *SpeedTestConfig, onResult func(LinkTestResult)) LinkTestSummary {
	var success, failed int32
	conc := resolveFixedConcurrency(cfg.LatencyConcurrency, 20, 1000)
	useAdaptive := cfg.LatencyConcurrency == 0 && adaptiveConcurrencyFactory != nil
	var controller AdaptiveConcurrencyController
	if useAdaptive {
		controller = newAdaptiveConcurrencyController(AdaptiveTypeLatency, len(items))
		conc = controller.GetCurrentConcurrency()
	}

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup

	for i := range items {
		item := items[i]
		if strings.TrimSpace(item.Link) == "" {
			emitResult(onResult, LinkTestResult{
				ID: item.ID, Link: item.Link, Name: item.Name,
				DelayTime: 0, DelayStatus: constants.StatusError,
				Speed: item.PrevSpeed, SpeedStatus: item.PrevSpeedStatus,
				IsValid: false, Err: fmt.Errorf("empty link"),
			}, false, &success, &failed)
			continue
		}
		select {
		case <-ctx.Done():
			emitResult(onResult, LinkTestResult{
				ID: item.ID, Link: item.Link, Name: item.Name,
				DelayTime: item.PrevDelayTime, DelayStatus: constants.StatusError,
				Speed: item.PrevSpeed, SpeedStatus: item.PrevSpeedStatus,
				IsValid: false, Err: ctx.Err(),
			}, false, &success, &failed)
			continue
		default:
		}

		wg.Add(1)
		go func(it LinkTestItem) {
			defer wg.Done()
			if useAdaptive && controller != nil {
				controller.AcquireWithDelay()
				defer controller.ReleaseDynamic()
			} else {
				sem <- struct{}{}
				defer func() { <-sem }()
			}

			latency, _, _, err := mihomo.MihomoDelayTest(
				it.Link,
				cfg.LatencyTestURL,
				cfg.Timeout,
				cfg.IncludeHandshake,
				false,
				"",
				false,
				"",
			)
			result := LinkTestResult{
				ID:          it.ID,
				Link:        it.Link,
				Name:        it.Name,
				Speed:       it.PrevSpeed,
				SpeedStatus: it.PrevSpeedStatus,
			}
			ok := err == nil && latency > 0
			if !ok {
				result.DelayTime = 0
				result.DelayStatus = constants.StatusError
				result.IsValid = false
				result.Err = err
				if useAdaptive && controller != nil {
					controller.ReportFailure()
					controller.MaybeAdjust()
				}
			} else {
				result.DelayTime = latency
				result.DelayStatus = constants.StatusSuccess
				result.IsValid = true
				if useAdaptive && controller != nil {
					controller.ReportSuccess(latency)
					controller.MaybeAdjust()
				}
			}
			emitResult(onResult, result, ok, &success, &failed)
		}(item)
	}
	wg.Wait()

	sum := LinkTestSummary{Total: len(items), Success: int(success), Failed: int(failed)}
	utils.Info("通用链路延时测试完成: total=%d success=%d failed=%d", sum.Total, sum.Success, sum.Failed)
	return sum
}

func runLinkSpeedOnly(ctx context.Context, items []LinkTestItem, cfg *SpeedTestConfig, onResult func(LinkTestResult)) LinkTestSummary {
	var success, failed int32
	conc := resolveFixedConcurrency(cfg.SpeedConcurrency, 5, 32)
	useAdaptive := cfg.SpeedConcurrency == 0 && adaptiveConcurrencyFactory != nil
	var controller AdaptiveConcurrencyController
	if useAdaptive {
		controller = newAdaptiveConcurrencyController(AdaptiveTypeSpeed, len(items))
		conc = controller.GetCurrentConcurrency()
		if conc > 32 {
			conc = 32
		}
	}

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup

	for i := range items {
		item := items[i]
		if strings.TrimSpace(item.Link) == "" {
			emitResult(onResult, LinkTestResult{
				ID: item.ID, Link: item.Link, Name: item.Name,
				DelayTime: item.PrevDelayTime, DelayStatus: item.PrevDelayStatus,
				Speed: 0, SpeedStatus: constants.StatusError,
				IsValid: item.PrevIsValid, Err: fmt.Errorf("empty link"),
			}, false, &success, &failed)
			continue
		}
		select {
		case <-ctx.Done():
			emitResult(onResult, LinkTestResult{
				ID: item.ID, Link: item.Link, Name: item.Name,
				DelayTime: item.PrevDelayTime, DelayStatus: item.PrevDelayStatus,
				Speed: 0, SpeedStatus: constants.StatusError,
				IsValid: item.PrevIsValid, Err: ctx.Err(),
			}, false, &success, &failed)
			continue
		default:
		}

		wg.Add(1)
		go func(it LinkTestItem) {
			defer wg.Done()
			if useAdaptive && controller != nil {
				controller.AcquireWithDelay()
				defer controller.ReleaseDynamic()
			} else {
				sem <- struct{}{}
				defer func() { <-sem }()
			}

			speed, latency, _, _, _, err := mihomo.MihomoSpeedTest(
				it.Link,
				cfg.SpeedTestURL,
				cfg.Timeout,
				false,
				"",
				false,
				"",
				cfg.SpeedRecordMode,
				cfg.PeakSampleInterval,
			)
			result := LinkTestResult{
				ID:          it.ID,
				Link:        it.Link,
				Name:        it.Name,
				DelayTime:   it.PrevDelayTime,
				DelayStatus: it.PrevDelayStatus,
				IsValid:     it.PrevIsValid,
			}
			ok := err == nil && speed > 0
			if !ok {
				result.Speed = 0
				result.SpeedStatus = constants.StatusError
				result.Err = err
				if useAdaptive && controller != nil {
					controller.ReportFailure()
					controller.MaybeAdjust()
				}
			} else {
				result.Speed = speed
				result.SpeedStatus = constants.StatusSuccess
				if latency > 0 {
					result.DelayTime = latency
					result.DelayStatus = constants.StatusSuccess
				}
				result.IsValid = true
				if useAdaptive && controller != nil {
					controller.ReportSuccess(int(speed * 1000))
					controller.MaybeAdjust()
				}
			}
			emitResult(onResult, result, ok, &success, &failed)
		}(item)
	}
	wg.Wait()

	sum := LinkTestSummary{Total: len(items), Success: int(success), Failed: int(failed)}
	utils.Info("通用链路测速完成: total=%d success=%d failed=%d", sum.Total, sum.Success, sum.Failed)
	return sum
}

func runLinkBoth(ctx context.Context, items []LinkTestItem, cfg *SpeedTestConfig, onResult func(LinkTestResult)) LinkTestSummary {
	// 先并发测延时，通过的再测速；最终只对测速成功的条目回调（抓取入库语义）
	type delayOK struct {
		item    LinkTestItem
		latency int
	}
	passed := make([]delayOK, 0, len(items))
	var mu sync.Mutex
	var tested int32

	delayConc := resolveFixedConcurrency(cfg.LatencyConcurrency, 20, 1000)
	sem := make(chan struct{}, delayConc)
	var wg sync.WaitGroup

	for i := range items {
		item := items[i]
		if strings.TrimSpace(item.Link) == "" {
			continue
		}
		select {
		case <-ctx.Done():
			goto AFTER_DELAY
		default:
		}
		wg.Add(1)
		go func(it LinkTestItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			atomic.AddInt32(&tested, 1)
			latency, _, _, err := mihomo.MihomoDelayTest(
				it.Link, cfg.LatencyTestURL, cfg.Timeout, cfg.IncludeHandshake, false, "", false, "",
			)
			if err != nil || latency <= 0 {
				return
			}
			mu.Lock()
			passed = append(passed, delayOK{item: it, latency: latency})
			mu.Unlock()
		}(item)
	}
AFTER_DELAY:
	wg.Wait()

	var success, failed int32
	speedConc := resolveFixedConcurrency(cfg.SpeedConcurrency, 5, 32)
	speedSem := make(chan struct{}, speedConc)
	var speedWg sync.WaitGroup

	for _, p := range passed {
		select {
		case <-ctx.Done():
			goto DONE
		default:
		}
		speedWg.Add(1)
		go func(d delayOK) {
			defer speedWg.Done()
			speedSem <- struct{}{}
			defer func() { <-speedSem }()

			speed, latency, _, _, _, err := mihomo.MihomoSpeedTest(
				d.item.Link, cfg.SpeedTestURL, cfg.Timeout, false, "", false, "",
				cfg.SpeedRecordMode, cfg.PeakSampleInterval,
			)
			result := LinkTestResult{
				ID: d.item.ID, Link: d.item.Link, Name: d.item.Name,
				DelayTime: d.latency, DelayStatus: constants.StatusSuccess,
			}
			if latency > 0 {
				result.DelayTime = latency
			}
			if err != nil || speed <= 0 {
				result.Speed = 0
				result.SpeedStatus = constants.StatusError
				result.IsValid = false
				result.Err = err
				atomic.AddInt32(&failed, 1)
				// both 模式失败默认不回调（抓取场景不入库）
				return
			}
			result.Speed = speed
			result.SpeedStatus = constants.StatusSuccess
			result.IsValid = true
			atomic.AddInt32(&success, 1)
			if onResult != nil {
				onResult(result)
			}
		}(p)
	}
DONE:
	speedWg.Wait()

	delayFailed := int(tested) - len(passed)
	sum := LinkTestSummary{
		Total:   len(items),
		Success: int(success),
		Failed:  delayFailed + int(failed),
	}
	utils.Info("通用链路延时+测速完成: total=%d delayPassed=%d success=%d failed=%d",
		sum.Total, len(passed), sum.Success, sum.Failed)
	return sum
}

// runLinkFull 按节点检测策略执行全测：先并发延时（全量回调），非 tcp 模式再对延时成功节点测速（回调）。
func runLinkFull(ctx context.Context, items []LinkTestItem, cfg *SpeedTestConfig, onResult func(LinkTestResult)) LinkTestSummary {
	var success, failed int32

	// ----- 阶段一：延时 -----
	type delayPass struct {
		item    LinkTestItem
		latency int
	}
	passed := make([]delayPass, 0, len(items))
	var mu sync.Mutex

	delayConc := resolveFixedConcurrency(cfg.LatencyConcurrency, 20, 1000)
	useAdaptiveDelay := cfg.LatencyConcurrency == 0 && adaptiveConcurrencyFactory != nil
	var delayController AdaptiveConcurrencyController
	if useAdaptiveDelay {
		delayController = newAdaptiveConcurrencyController(AdaptiveTypeLatency, len(items))
		delayConc = delayController.GetCurrentConcurrency()
	}
	delaySem := make(chan struct{}, delayConc)
	var delayWg sync.WaitGroup

	for i := range items {
		item := items[i]
		if strings.TrimSpace(item.Link) == "" {
			emitResult(onResult, LinkTestResult{
				ID: item.ID, Link: item.Link, Name: item.Name,
				DelayTime: 0, DelayStatus: constants.StatusError,
				Speed: item.PrevSpeed, SpeedStatus: item.PrevSpeedStatus,
				IsValid: false, Err: fmt.Errorf("empty link"),
			}, false, &success, &failed)
			continue
		}
		select {
		case <-ctx.Done():
			emitResult(onResult, LinkTestResult{
				ID: item.ID, Link: item.Link, Name: item.Name,
				DelayTime: item.PrevDelayTime, DelayStatus: constants.StatusError,
				Speed: item.PrevSpeed, SpeedStatus: item.PrevSpeedStatus,
				IsValid: false, Err: ctx.Err(),
			}, false, &success, &failed)
			continue
		default:
		}

		delayWg.Add(1)
		go func(it LinkTestItem) {
			defer delayWg.Done()
			if useAdaptiveDelay && delayController != nil {
				delayController.AcquireWithDelay()
				defer delayController.ReleaseDynamic()
			} else {
				delaySem <- struct{}{}
				defer func() { <-delaySem }()
			}

			latency, _, _, err := mihomo.MihomoDelayTest(
				it.Link, cfg.LatencyTestURL, cfg.Timeout, cfg.IncludeHandshake, false, "", false, "",
			)
			result := LinkTestResult{
				ID: it.ID, Link: it.Link, Name: it.Name,
				Speed: it.PrevSpeed, SpeedStatus: it.PrevSpeedStatus,
			}
			ok := err == nil && latency > 0
			if !ok {
				result.DelayTime = 0
				result.DelayStatus = constants.StatusError
				result.IsValid = false
				result.Err = err
				if useAdaptiveDelay && delayController != nil {
					delayController.ReportFailure()
					delayController.MaybeAdjust()
				}
				emitResult(onResult, result, false, &success, &failed)
				return
			}
			result.DelayTime = latency
			result.DelayStatus = constants.StatusSuccess
			result.IsValid = true
			if useAdaptiveDelay && delayController != nil {
				delayController.ReportSuccess(latency)
				delayController.MaybeAdjust()
			}
			// TCP 模式到此结束；mihomo 模式先回调延时，再进入测速
			if strings.EqualFold(cfg.Mode, "tcp") {
				emitResult(onResult, result, true, &success, &failed)
				return
			}
			// mihomo：延时成功暂不计入最终 success，等测速；但仍先写出延时
			if onResult != nil {
				onResult(result)
			}
			mu.Lock()
			passed = append(passed, delayPass{item: it, latency: latency})
			mu.Unlock()
		}(item)
	}
	delayWg.Wait()

	if strings.EqualFold(cfg.Mode, "tcp") || len(passed) == 0 {
		sum := LinkTestSummary{Total: len(items), Success: int(success), Failed: int(failed)}
		// 对 mihomo 且无人通过延时：failed 已在上面统计
		if !strings.EqualFold(cfg.Mode, "tcp") && len(passed) == 0 {
			// 延时失败数 = total - empty already counted
			sum.Failed = len(items) - int(success)
			if sum.Failed < 0 {
				sum.Failed = int(failed)
			}
		}
		utils.Info("通用全测完成(mode=%s): total=%d success=%d failed=%d", cfg.Mode, sum.Total, sum.Success, sum.Failed)
		return sum
	}

	// ----- 阶段二：测速（仅延时成功）-----
	// 重置计数：最终以测速成功为准（延时失败已计入 failed）
	speedSuccess := int32(0)
	speedFailed := int32(0)
	speedConc := resolveFixedConcurrency(cfg.SpeedConcurrency, 5, 32)
	useAdaptiveSpeed := cfg.SpeedConcurrency == 0 && adaptiveConcurrencyFactory != nil
	var speedController AdaptiveConcurrencyController
	if useAdaptiveSpeed {
		speedController = newAdaptiveConcurrencyController(AdaptiveTypeSpeed, len(passed))
		speedConc = speedController.GetCurrentConcurrency()
		if speedConc > 32 {
			speedConc = 32
		}
	}
	speedSem := make(chan struct{}, speedConc)
	var speedWg sync.WaitGroup

	for _, p := range passed {
		select {
		case <-ctx.Done():
			goto DONE
		default:
		}
		speedWg.Add(1)
		go func(d delayPass) {
			defer speedWg.Done()
			if useAdaptiveSpeed && speedController != nil {
				speedController.AcquireWithDelay()
				defer speedController.ReleaseDynamic()
			} else {
				speedSem <- struct{}{}
				defer func() { <-speedSem }()
			}

			speed, latency, _, _, _, err := mihomo.MihomoSpeedTest(
				d.item.Link, cfg.SpeedTestURL, cfg.Timeout, false, "", false, "",
				cfg.SpeedRecordMode, cfg.PeakSampleInterval,
			)
			result := LinkTestResult{
				ID: d.item.ID, Link: d.item.Link, Name: d.item.Name,
				DelayTime: d.latency, DelayStatus: constants.StatusSuccess,
			}
			if latency > 0 {
				result.DelayTime = latency
			}
			ok := err == nil && speed > 0
			if !ok {
				result.Speed = 0
				result.SpeedStatus = constants.StatusError
				result.IsValid = false
				result.Err = err
				if useAdaptiveSpeed && speedController != nil {
					speedController.ReportFailure()
					speedController.MaybeAdjust()
				}
				emitResult(onResult, result, false, &speedSuccess, &speedFailed)
				return
			}
			result.Speed = speed
			result.SpeedStatus = constants.StatusSuccess
			result.IsValid = true
			if useAdaptiveSpeed && speedController != nil {
				speedController.ReportSuccess(int(speed * 1000))
				speedController.MaybeAdjust()
			}
			emitResult(onResult, result, true, &speedSuccess, &speedFailed)
		}(p)
	}
DONE:
	speedWg.Wait()

	sum := LinkTestSummary{
		Total:   len(items),
		Success: int(speedSuccess),
		Failed:  len(items) - int(speedSuccess),
	}
	utils.Info("通用全测完成(mode=%s): total=%d success=%d failed=%d delayPassed=%d",
		cfg.Mode, sum.Total, sum.Success, sum.Failed, len(passed))
	return sum
}
