package settings

import (
	"sublink/models"
	"sublink/utils"
)

// 重置默认用户 - 只更新admin用户的密码，不删除其他用户
func ResetUser(username string, password string) {
	// 如果账号或者密码为空
	if username == "" || password == "" {
		utils.Error("账号或者密码不能为空")
		return
	}
	if len(password) < 6 {
		utils.Error("密码不能小于6位数")
		return
	}

	// 只更新admin用户的密码
	user := &models.User{Username: username}
	if err := user.Find(); err == nil {
		user.Password = password
		_ = user.Set(user) // reuse Set which will hash it
		utils.Info("已更新管理员密码")
		return
	}

	// 如果没有admin用户，则创建
	User := &models.User{Username: username, Password: password, Role: "admin", Nickname: "管理员"}
	_ = User.Create()
}
