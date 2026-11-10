// Command dirprobe shows what the operating system answers for a directory:
// os.Lstat, os.Stat, os.Open, Readdirnames, os.ReadDir and access(2), each
// with the kind of the error (errors.Is ErrNotExist, ErrPermission, the
// errno), and what total-recall makes of it (store.LastBackup,
// store.ReadBackupStatus). It is for finding out why a backup directory
// shows as none or unknown:
//
//	go run ./tools/dirprobe DIR [DB]
//
// It only reads; it never writes to DIR.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/seletz/total-recall/internal/store"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/dirprobe DIR [DB]")
		os.Exit(2)
	}
	dir := os.Args[1]
	home, _ := os.UserHomeDir()
	if dir == "~" || len(dir) > 1 && dir[:2] == "~/" {
		fmt.Printf("note: %q starts with ~ and is NOT expanded by this program; the shell expands it unquoted\n", dir)
	}
	fmt.Printf("uid %d euid %d gid %d, home %s\n", os.Getuid(), os.Geteuid(), os.Getgid(), home)
	fmt.Printf("dir  %s\n", dir)
	if abs, err := filepath.Abs(dir); err == nil {
		fmt.Printf("abs  %s\n", abs)
	}
	real, err := filepath.EvalSymlinks(dir)
	show("EvalSymlinks", real, err)

	fi, err := os.Lstat(dir)
	showInfo("os.Lstat", fi, err)
	fi, err = os.Stat(dir)
	showInfo("os.Stat", fi, err)
	show("access R_OK|X_OK", "", syscall.Access(dir, 0x4|0x1))

	f, err := os.Open(dir)
	show("os.Open", "", err)
	if err == nil {
		names, err := f.Readdirnames(-1)
		show("Readdirnames(-1)", fmt.Sprintf("%d names", len(names)), err)
		_ = f.Close()
	}
	ents, err := os.ReadDir(dir)
	show("os.ReadDir", fmt.Sprintf("%d entries", len(ents)), err)
	if err == nil {
		for i, e := range ents {
			if i == 5 {
				fmt.Println("  ...")
				break
			}
			_, ierr := e.Info()
			show("  entry Info "+e.Name(), "", ierr)
		}
	}
	f2, err := os.Open(dir)
	if err == nil {
		_, rerr := f2.Readdirnames(1)
		show("Readdirnames(1) (EOF = empty)", "", rerr)
		_ = f2.Close()
	}

	fmt.Println("--- what total-recall makes of it")
	p, at, err := store.LastBackup(dir, time.Local)
	show("store.LastBackup", fmt.Sprintf("path %q at %v", p, at), err)
	db := ""
	if len(os.Args) == 3 {
		db = os.Args[2]
	}
	st := store.ReadBackupStatus(store.BackupProbe{DB: db, Dir: dir, Loc: time.Local, Now: time.Now(), Home: home})
	fmt.Printf("store.ReadBackupStatus  state=%s reason=%q\n  hint=%q\n  path=%q at=%v\n", st.State, st.Reason, st.Hint, st.Path, st.At)
}

func showInfo(op string, fi fs.FileInfo, err error) {
	v := ""
	if fi != nil {
		v = fmt.Sprintf("mode %v dir=%v", fi.Mode(), fi.IsDir())
	}
	show(op, v, err)
}

// show prints one call: the value, or the error with its kinds.
func show(op, value string, err error) {
	if err == nil {
		fmt.Printf("%-30s ok %s\n", op, value)
		return
	}
	if errors.Is(err, io.EOF) {
		fmt.Printf("%-30s EOF (empty directory)\n", op)
		return
	}
	var errno syscall.Errno
	no := "-"
	if errors.As(err, &errno) {
		no = fmt.Sprintf("errno %d (%s)", int(errno), errno.Error())
	}
	fmt.Printf("%-30s ERROR %v\n   NotExist=%v Permission=%v Exist=%v %s\n", op, err,
		errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission), errors.Is(err, fs.ErrExist), no)
}
