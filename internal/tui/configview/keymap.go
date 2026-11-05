package configview

import (
	"charm.land/bubbles/v2/key"

	"github.com/seletz/total-recall/internal/tui/keys"
)

// KeyMap holds the bindings of the config view, built from the effective
// keys (see package keys). The pages pick what they use; the footer shows
// the page switch as one group.
type KeyMap struct {
	Next, Prev  key.Binding
	Up, Down    key.Binding
	Select      key.Binding
	Default     key.Binding
	Confirm     key.Binding
	Copy        key.Binding
	Editor      key.Binding
	Add, Remove key.Binding
	MoveUp      key.Binding
	MoveDn      key.Binding
	Pattern     key.Binding
	Back        key.Binding
	ForceQuit   key.Binding
	Pages, Move key.Binding
}

func newKeyMap(m keys.Map) KeyMap {
	return KeyMap{
		Next:      m.Binding(keys.ConfigNext),
		Prev:      m.Binding(keys.ConfigPrev),
		Up:        m.Binding(keys.ConfigUp),
		Down:      m.Binding(keys.ConfigDown),
		Select:    m.Binding(keys.ConfigSelect),
		Default:   m.Binding(keys.ConfigDefault),
		Confirm:   m.Binding(keys.ConfigConfirm),
		Copy:      m.Binding(keys.ConfigCopy),
		Editor:    m.Binding(keys.ConfigEditor),
		Add:       m.Binding(keys.ConfigAdd),
		Remove:    m.Binding(keys.ConfigRemove),
		MoveUp:    m.Binding(keys.ConfigMoveUp),
		MoveDn:    m.Binding(keys.ConfigMoveDn),
		Pattern:   m.Binding(keys.ConfigPattern),
		Back:      m.Binding(keys.GlobalBack),
		ForceQuit: keys.ForceQuit,
		// The footer names tab only; the help lists shift+tab as well.
		Pages: m.Group("page", keys.ConfigNext),
		Move:  m.Group("move", keys.ConfigUp, keys.ConfigDown),
	}
}
