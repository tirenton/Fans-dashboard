package main

// `api user ...`: manage dashboard accounts in USERS_FILE. Run it in the api container, e.g.
//   docker compose -p nmixx-fans run --rm -it api user add alice admin
// Passwords are typed at a hidden prompt (or read from stdin when it is not a terminal) and stored only as
// argon2id hashes.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const userUsage = `usage: api user <command>
  list                     list accounts and roles
  add <name> <admin|user>  create an account (asks for its password)
  passwd <name>            set a new password
  role <name> <admin|user> change an account's role
  rm <name>                delete an account
  email <name> <address|-> set an account's email (for password resets and logging in by email); - clears it
names: lowercase letters, digits, . _ - (up to 32); passwords: at least 10 characters`

func runUserCmd(file string, args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", userUsage)
	}
	users, err := LoadUsers(file)
	if err != nil {
		return err
	}
	find := func(name string) int {
		for i, u := range users {
			if u.Name == name {
				return i
			}
		}
		return -1
	}
	need := func(n int) error {
		if len(args) != n {
			return fmt.Errorf("%s", userUsage)
		}
		return nil
	}
	switch args[0] {
	case "list":
		if len(users) == 0 {
			fmt.Fprintln(out, "no accounts yet; add one with: api user add <name> admin")
		}
		for _, u := range users {
			fmt.Fprintf(out, "%-32s %-5s  %s\n", u.Name, u.Role, u.Email)
		}
		return nil
	case "email":
		if err := need(3); err != nil {
			return err
		}
		i := find(strings.ToLower(args[1]))
		if i < 0 {
			return fmt.Errorf("no account %q", args[1])
		}
		email := ""
		if args[2] != "-" {
			e, err := normEmail(args[2])
			if err != nil {
				return fmt.Errorf("%q is not an email address", args[2])
			}
			for j, u := range users {
				if j != i && u.Email == e {
					return fmt.Errorf("%s already belongs to %s", e, u.Name)
				}
			}
			email = e
		}
		users[i].Email = email
	case "add", "role":
		if err := need(3); err != nil {
			return err
		}
		name, role := strings.ToLower(args[1]), args[2]
		if !userNameRe.MatchString(name) {
			return fmt.Errorf("bad name %q: lowercase letters, digits, . _ - (up to 32)", args[1])
		}
		if !validRole(role) {
			return fmt.Errorf("role must be admin or user")
		}
		i := find(name)
		if args[0] == "role" {
			if i < 0 {
				return fmt.Errorf("no account %q", name)
			}
			users[i].Role = role
			break
		}
		if i >= 0 {
			return fmt.Errorf("account %q exists; use passwd or role", name)
		}
		hash, err := askPassword(in, out)
		if err != nil {
			return err
		}
		users = append(users, User{Name: name, Role: role, Hash: hash})
	case "passwd":
		if err := need(2); err != nil {
			return err
		}
		i := find(strings.ToLower(args[1]))
		if i < 0 {
			return fmt.Errorf("no account %q", args[1])
		}
		hash, err := askPassword(in, out)
		if err != nil {
			return err
		}
		users[i].Hash = hash
	case "rm":
		if err := need(2); err != nil {
			return err
		}
		i := find(strings.ToLower(args[1]))
		if i < 0 {
			return fmt.Errorf("no account %q", args[1])
		}
		users = append(users[:i], users[i+1:]...)
	default:
		return fmt.Errorf("%s", userUsage)
	}
	if err := SaveUsers(file, users); err != nil {
		return err
	}
	fmt.Fprintln(out, "saved", file)
	return nil
}

// askPassword reads a new password twice (hidden on a terminal) and returns its hash.
func askPassword(in io.Reader, out io.Writer) (string, error) {
	var read func(prompt string) (string, error)
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		read = func(prompt string) (string, error) {
			fmt.Fprint(out, prompt)
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(out)
			return string(b), err
		}
	} else {
		r := bufio.NewReader(in)
		read = func(string) (string, error) {
			s, err := r.ReadString('\n')
			if err != nil && s == "" {
				return "", fmt.Errorf("no password given")
			}
			return strings.TrimRight(s, "\r\n"), nil
		}
	}
	pw, err := read("Password: ")
	if err != nil {
		return "", err
	}
	if len(pw) < MinPasswordLen {
		return "", fmt.Errorf("password too short: at least %d characters", MinPasswordLen)
	}
	again, err := read("Again: ")
	if err != nil {
		return "", err
	}
	if again != pw {
		return "", fmt.Errorf("passwords differ")
	}
	return HashPassword(pw)
}
