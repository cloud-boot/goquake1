module github.com/go-quake1/engine

go 1.26.4

require (
	github.com/go-virtio/gpu v0.0.0
	github.com/go-virtio/input v0.0.0
	github.com/go-virtio/sound v0.0.0
)

require github.com/go-virtio/common v0.1.5 // indirect

replace (
	github.com/go-virtio/common => ../../go-virtio/common
	github.com/go-virtio/gpu => ../../go-virtio/gpu
	github.com/go-virtio/input => ../../go-virtio/input
	github.com/go-virtio/sound => ../../go-virtio/sound
)
