package camellia

import (
	"fmt"
	"sync/atomic"
)

type hookType uint

const (
	hookTypePre  hookType = 1
	hookTypePost hookType = 2
)

type hook struct {
	callback func(path string, value string) error
	async    bool
	hT       hookType
}

var hooksEnabled = uint32(1)
var hooksEmpty = uint32(1)

var hooks = map[hookType]map[string][]*hook{}

func SetHooksEnabled(enabled bool) {
	if enabled {
		atomic.StoreUint32(&hooksEnabled, 1)
	} else {
		atomic.StoreUint32(&hooksEnabled, 0)
	}
}

func SetPreSetHook(path string, callback func(path string, value string) error) error {
	return setHook(path, callback, false, hookTypePre)
}

func SetPostSetHook(path string, callback func(path string, value string) error, async bool) error {
	return setHook(path, callback, async, hookTypePost)
}

func callHooks(path string, value string, hT hookType) error {
	if atomic.LoadUint32(&hooksEnabled) != 1 {
		return nil
	}

	if atomic.LoadUint32(&hooksEmpty) == 1 {
		return nil
	}

	mutex.Lock()
	defer mutex.Unlock()

	if hooks[hT] != nil && hooks[hT][path] != nil {
		for i, h := range hooks[hT][path] {
			if h != nil {
				if !h.async {
					err := h.callback(path, value)
					if err != nil {
						switch hT {
						case hookTypePre:
							return fmt.Errorf("error calling pre set hook %d - %w", i, err)
						case hookTypePost:
							return fmt.Errorf("error calling post set hook %d - %w", i, err)
						default:
							return fmt.Errorf("error calling UKNOWN TYPE hook %d - %w", i, err)
						}
					}
				} else {
					go h.callback(path, value)
				}
			}
		}
	}

	return nil
}

func callPreSetHook(path string, value string) error {
	return callHooks(path, value, hookTypePre)
}

func callPostSetHook(path string, value string) error {
	return callHooks(path, value, hookTypePost)
}

func setHook(path string, callback func(path string, value string) error, async bool, hT hookType) error {
	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNotInitialized
	}

	mutex.Lock()
	defer mutex.Unlock()

	if hooks[hT] == nil {
		hooks[hT] = make(map[string][]*hook)
	}

	hooks[hT][path] = append(hooks[hT][path], &hook{
		callback: callback,
		async:    async,
		hT:       hT})

	atomic.StoreUint32(&hooksEmpty, 0)

	return nil
}
