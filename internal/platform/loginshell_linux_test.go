package platform

import "testing"

func TestPasswdShell(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/bash\n# comment\nbad line\nronel:x:1000:1000::/home/ronel:/usr/bin/zsh\nnosh:x:1001:1001::/home/n:\n"
	for uid, want := range map[int]string{0: "/bin/bash", 1000: "/usr/bin/zsh"} {
		if got, ok := passwdShell(passwd, uid); !ok || got != want {
			t.Errorf("uid %d: %q %v", uid, got, ok)
		}
	}
	for _, uid := range []int{1001, 4242} {
		if got, ok := passwdShell(passwd, uid); ok {
			t.Errorf("uid %d: %q", uid, got)
		}
	}
	if sh, ok := LoginShell(); !ok || sh == "" {
		t.Errorf("LoginShell on this machine: %q %v", sh, ok)
	}
}
