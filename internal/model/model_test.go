package model

import "testing"

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"kind message", KindMessage.Valid(), true},
		{"kind meta", KindMeta.Valid(), true},
		{"kind unknown", Kind("note").Valid(), false},
		{"kind empty", Kind("").Valid(), false},
		{"actor human", ActorHuman.Valid(), true},
		{"actor tool", ActorTool.Valid(), true},
		{"actor unknown", Actor("robot").Valid(), false},
		{"role user", ValidRole("user"), true},
		{"role tool", ValidRole("tool"), true},
		{"role unknown", ValidRole("bot"), false},
		{"role empty", ValidRole(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
