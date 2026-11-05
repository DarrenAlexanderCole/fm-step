// Package link defines the supervisor <-> application protocol.
//
// The supervisor starts the application with the write end of a pipe as
// file descriptor 3 (FMSTEP_LINK_FD). The application writes single bytes:
//
//	'R'  ready: audio, display and control surface are open and working
//	'H'  heartbeat: sent at least every 250 ms from the main loop
//	     (NOT from the audio thread, so a hung UI/main loop is detected)
//	'X'  exit to stock: sequencing stopped, voices released, project saved,
//	     audio/hardware released. The app exits right after sending it.
//	'B'  reboot requested by the user (controlled reboot)
//
// EOF on the pipe means the application exited or crashed.
// The protocol is one byte per message so a C/C++ application can implement
// it with a single write(3, &c, 1).
package link

const (
	FD = 3

	Ready     byte = 'R'
	Heartbeat byte = 'H'
	ExitStock byte = 'X'
	Reboot    byte = 'B'
)
