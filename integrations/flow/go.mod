module github.com/zzliekkas/flow-storage/integrations/flow

go 1.23.0

toolchain go1.23.3

require (
	github.com/zzliekkas/flow-storage v0.1.0
	github.com/zzliekkas/flow/v2 v2.0.0
)

require (
	github.com/disintegration/imaging v1.6.2 // indirect
	go.uber.org/dig v1.17.0 // indirect
	golang.org/x/image v0.0.0-20191009234506-e7c1f5e7dbb8 // indirect
)

replace github.com/zzliekkas/flow-storage => ../..

replace github.com/zzliekkas/flow/v2 => ../../../flow
