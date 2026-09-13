// Package sftpruntime implements WindowsAgent's independent filesystem data
// plane. Filesystem authorization is the Windows access token of the
// windows-sftp process plus normal Windows filesystem semantics; it is not
// derived from an interactive desktop, SSH username, or SFTP credential.
package sftpruntime
