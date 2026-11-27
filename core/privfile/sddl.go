package privfile

// sddlFor is the DACL of a private file (dir: a private dir, whose ACEs
// its children inherit) for the user sid. No owner: Windows makes the
// creator the owner (or Administrators, for an elevated admin under the
// default policy, who have full control here anyway), and an explicit one
// can be refused by a network share. Kept out of the
// Windows file so every platform's tests check it.
func sddlFor(sid string, dir bool) string {
	inh := ""
	if dir {
		inh = "OICI"
	}
	ace := func(who string) string { return "(A;" + inh + ";FA;;;" + who + ")" }
	s := "D:P" + ace(sid)
	if sid != "S-1-5-18" { // LocalSystem itself
		s += ace("SY")
	}
	if sid != "S-1-5-32-544" {
		s += ace("BA")
	}
	return s
}
