package cloud

import (
	"github.com/zzliekkas/flow-storage/core"
)

func init() {
	core.RegisterDriver("s3", func(config map[string]interface{}) (core.FileSystem, error) {
		return createS3Driver(config)
	})
	core.RegisterDriver("oss", func(config map[string]interface{}) (core.FileSystem, error) {
		return createOSSDriver(config)
	})
	core.RegisterDriver("cos", func(config map[string]interface{}) (core.FileSystem, error) {
		return createCOSDriver(config)
	})
	core.RegisterDriver("qiniu", func(config map[string]interface{}) (core.FileSystem, error) {
		return createQiniuDriver(config)
	})
}
