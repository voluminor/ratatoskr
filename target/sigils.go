// Code generated using '_generate/sigils'; DO NOT EDIT.

package target

import (
	"github.com/voluminor/ratatoskr/mod/sigils"
	inet "github.com/voluminor/ratatoskr/mod/sigils/inet"
	info "github.com/voluminor/ratatoskr/mod/sigils/info"
	public "github.com/voluminor/ratatoskr/mod/sigils/public"
	services "github.com/voluminor/ratatoskr/mod/sigils/services"
)

// // // // // // // // // //

var globalSigilParseMap = map[string]func(map[string]any) (sigils.Interface, error){
	inet.Name():     func(m map[string]any) (sigils.Interface, error) { return inet.Parse(m) },
	info.Name():     func(m map[string]any) (sigils.Interface, error) { return info.Parse(m) },
	public.Name():   func(m map[string]any) (sigils.Interface, error) { return public.Parse(m) },
	services.Name(): func(m map[string]any) (sigils.Interface, error) { return services.Parse(m) },
}

// // // // // // // // // //

// Parse returns the parser for a built-in sigil; ok reports its presence.
func Parse(name string) (func(map[string]any) (sigils.Interface, error), bool) {
	fn, ok := globalSigilParseMap[name]
	return fn, ok
}

// Sigils returns a new name-to-parser map of all built-in sigils; the caller may modify it freely.
func Sigils() map[string]func(map[string]any) (sigils.Interface, error) {
	return map[string]func(map[string]any) (sigils.Interface, error){
		inet.Name():     func(m map[string]any) (sigils.Interface, error) { return inet.Parse(m) },
		info.Name():     func(m map[string]any) (sigils.Interface, error) { return info.Parse(m) },
		public.Name():   func(m map[string]any) (sigils.Interface, error) { return public.Parse(m) },
		services.Name(): func(m map[string]any) (sigils.Interface, error) { return services.Parse(m) },
	}
}
