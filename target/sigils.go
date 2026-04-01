// Code generated using '_generate/sigils'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package target

import (
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/inet"
	"github.com/voluminor/ratatoskr/mod/sigils/info"
	"github.com/voluminor/ratatoskr/mod/sigils/public"
	"github.com/voluminor/ratatoskr/mod/sigils/services"
)

// // // // // // // //

var GlobalSigilKeysMap = map[string]func() []string{
	inet.Name():     inet.Keys,
	info.Name():     info.Keys,
	public.Name():   public.Keys,
	services.Name(): services.Keys,
}

var GlobalSigilParseParamsMap = map[string]func(map[string]any) map[string]any{
	inet.Name():     inet.ParseParams,
	info.Name():     info.ParseParams,
	public.Name():   public.ParseParams,
	services.Name(): services.ParseParams,
}

var GlobalSigilMatchMap = map[string]func(map[string]any) bool{
	inet.Name():     inet.Match,
	info.Name():     info.Match,
	public.Name():   public.Match,
	services.Name(): services.Match,
}

var GlobalSigilParseMap = map[string]func(map[string]any) (sigils.Interface, error){
	inet.Name():     func(m map[string]any) (sigils.Interface, error) { return inet.Parse(m) },
	info.Name():     func(m map[string]any) (sigils.Interface, error) { return info.Parse(m) },
	public.Name():   func(m map[string]any) (sigils.Interface, error) { return public.Parse(m) },
	services.Name(): func(m map[string]any) (sigils.Interface, error) { return services.Parse(m) },
}
