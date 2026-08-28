package godwarf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLoginSyntax(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "login")
	content := `@login {
message LOGIN_HI
@emv.textlog notify
call @emv.textlog notify
}

@"\emv.textlog" {
message EMV
}
`
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	macroInit()
	macroParseFile(f)
	found := map[string]bool{}
	for m := macroState.Functions; m != nil; m = m.Next {
		found[m.Name] = true
		t.Logf("function name=%q kind=%d", m.Name, m.Kind)
		for c := m.Commands; c != nil; c = c.Next {
			t.Logf("  cmd kind=%d var=%q", c.CommandKind, c.VarName)
		}
	}
	t.Logf("functions: %v", found)
	if !found["@login"] {
		t.Errorf("expected @login function to be parsed")
	}
	if !found["@emv.textlog"] && !found["@\"\\emv.textlog\""] {
		t.Errorf("expected @emv.textlog function; got %v", found)
	}
}