package privfile

// sddlFor is the security descriptor of a private file (dir: a private
// dir, whose ACEs its children inherit) owned by sid. Kept out of the
// Windows file so every platform's tests check it.
func sddlFor(sid string, dir bool) string {
	inh := ""
	if dir {
		inh = "OICI"
	}
	ace := func(who string) string { return "(A;" + inh + ";FA;;;" + who + ")" }
	s := "O:" + sid + "D:P" + ace(sid)
	if sid != "S-1-5-18" { // LocalSystem itself
		s += ace("SY")
	}
	if sid != "S-1-5-32-544" {
		s += ace("BA")
	}
	return s
}
