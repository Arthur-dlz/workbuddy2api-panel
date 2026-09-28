//go:build !windows

package tray

// Config 托盘参数。
type Config struct {
	Title  string
	Port   int
	APIKey string
	OnExit func()
}

// Run 在非 Windows 平台为空实现。
func Run(cfg Config) error {
	return nil
}

// Stop 在非 Windows 平台为空实现。
func Stop() {}
