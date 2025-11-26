// Package options 提供各种组件的配置选项
//
// 这个包定义了常用外部服务和组件的配置选项类型，包括：
//   - Redis 配置选项
//   - MySQL 配置选项
//   - PostgreSQL 配置选项
//
// 所有配置选项类型都实现了以下标准方法：
//   - Validate() - 验证配置的有效性
//   - Complete() - 补充默认值
//   - AddFlags() - 添加命令行标志支持
//
// 这些配置选项支持多种配置方式：
//   - 命令行标志 (via pflag)
//   - 配置文件 (via viper/mapstructure)
//   - 环境变量
//   - 代码直接设置
//
// 示例用法：
//
//	// 创建默认配置
//	opts := options.NewRedisOptions()
//
//	// 添加命令行标志
//	fs := pflag.NewFlagSet("app", pflag.ExitOnError)
//	opts.AddFlags(fs)
//	fs.Parse(os.Args[1:])
//
//	// 验证配置
//	if err := opts.Validate(); err != nil {
//	    log.Fatal(err)
//	}
//
//	// 补充默认值
//	if err := opts.Complete(); err != nil {
//	    log.Fatal(err)
//	}
//
// 此包属于 Layer 1 (Foundation)，不依赖任何其他 goagent 包
package options
