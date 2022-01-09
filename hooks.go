package camellia

import (
	"fmt"
	"sync"
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
var hooksMutex sync.Mutex

func SetHooksEnabled(enabled bool) {
	if enabled {
		atomic.StoreUint32(&hooksEnabled, 1)
	} else {
		atomic.StoreUint32(&hooksEnabled, 0)
	}
}

/*
SetPreSetHook registers a callback to be called before the value at the specified path is changed.

If one of the registered callbacks on a path returns an error, the setting of the value at that path fails.

Callbacks are called on the same thread executing the set operation, in the same order as they were registered.
*/
func SetPreSetHook(path string, callback func(path string, value string) error) error {
	hooksMutex.Lock()
	defer hooksMutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	return setHook(path, callback, false, hookTypePre)
}

/*
SetPreSetHook registers a callback to be called after the value at the specified path is changed.

If async == false, if one of the registered callbacks on a path returns an error,
the setting of the value at that path fails.

If async == true, the registered callback will be called inside a new goroutine, and its returned error is ignored.

Callback are always called in the same order as they were registered.
*/
func SetPostSetHook(path string, callback func(path string, value string) error, async bool) error {
	hooksMutex.Lock()
	defer hooksMutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	return setHook(path, callback, async, hookTypePost)
}

func callHooks(path string, value string, hT hookType) error {
	if atomic.LoadUint32(&hooksEnabled) != 1 {
		return nil
	}

	if atomic.LoadUint32(&hooksEmpty) == 1 {
		return nil
	}

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
							return fmt.Errorf("error calling UNKNOWN TYPE hook %d - %w", i, err)
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

func wipeHooks() {
	hooksMutex.Lock()
	defer hooksMutex.Unlock()

	hooks = map[hookType]map[string][]*hook{}
}

func callPreSetHooks(path string, value string) error {
	return callHooks(path, value, hookTypePre)
}

func callPostSetHooks(path string, value string) error {
	return callHooks(path, value, hookTypePost)
}

func setHook(path string, callback func(path string, value string) error, async bool, hT hookType) error {
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
