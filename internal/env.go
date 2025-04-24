package internal

import (
	"fmt"
	"os"
)

var (
	_InjectValue string
)

// GetInjectKey 返回Exec服务时应当注入的环境变量名称
func GetInjectKey() string {
	var key = fmt.Sprintf("_ALLY_UNIX_SOCK_%d", os.Getpid())
	return key
}

// GetInjectValue 从环境变量中获取到来自Ally注入的通信地址信息
func GetInjectValue() string {
	var key = fmt.Sprintf("_ALLY_UNIX_SOCK_%d", os.Getppid())
	if val, ok := os.LookupEnv(key); ok {
		_InjectValue = val
		os.Unsetenv(key)
	}
	return _InjectValue
}
