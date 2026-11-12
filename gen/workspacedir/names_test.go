package workspacedir

import "testing"

func TestDirectoryNames(t *testing.T) {
	for _, s := range []string{"test", "用户一", "dev@example.com", "game build (2)"} {
		if !ValidName(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", ".", "..", "a..b", `C:\code`, `apps\repo`, "../repo", "con.txt", "LPT1", "COM¹.txt", "CONIN$", "file:stream", "a.", "username ", "USERNA~1"} {
		if ValidName(s) {
			t.Fatal(s)
		}
	}
}
func TestConfiguredRoots(t *testing.T) {
	if err := ValidateRoots([]string{`E:\`, `D:\Workspaces`}); err != nil {
		t.Fatal(err)
	}
	for _, roots := range [][]string{{`E:relative`}, {`D:\One`, `d:/one/two`}, {"/root/../etc"}, {`\\?\Volume`}} {
		if ValidateRoots(roots) == nil {
			t.Fatal(roots)
		}
	}
}
