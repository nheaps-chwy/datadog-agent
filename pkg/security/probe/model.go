// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

// +build linux

package probe

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"
	"time"

	pconfig "github.com/DataDog/datadog-agent/pkg/process/config"
	"github.com/DataDog/datadog-agent/pkg/security/model"
	"github.com/DataDog/datadog-agent/pkg/security/secl/eval"
)

// Model describes the data model for the runtime security agent probe events
type Model struct {
	model.Model
}

// NewEvent returns a new Event
func (m *Model) NewEvent() eval.Event {
	return &Event{Event: model.Event{}}
}

// Event describes a probe event
type Event struct {
	model.Event

	resolvers           *Resolvers
	processCacheEntry   *model.ProcessCacheEntry
	pathResolutionError error
	scrubber            *pconfig.DataScrubber
}

// GetPathResolutionError returns the path resolution error as a string if there is one
func (ev *Event) GetPathResolutionError() error {
	return ev.pathResolutionError
}

// ResolveFileInode resolves the inode to a full path
func (ev *Event) ResolveFileInode(f *model.FileEvent) string {
	if len(f.PathnameStr) == 0 {
		path, err := ev.resolvers.resolveInode(&f.FileFields)
		if err != nil {
			if _, ok := err.(ErrTruncatedSegment); ok {
				ev.SetPathResolutionError(err)
			} else if _, ok := err.(ErrTruncatedParents); ok {
				ev.SetPathResolutionError(err)
			}
		}
		f.PathnameStr = path
	}
	return f.PathnameStr
}

// ResolveFileBasename resolves the inode to a full path
func (ev *Event) ResolveFileBasename(f *model.FileEvent) string {
	if len(f.BasenameStr) == 0 {
		if f.PathnameStr != "" {
			f.BasenameStr = path.Base(f.PathnameStr)
		} else {
			f.BasenameStr = ev.resolvers.resolveBasename(&f.FileFields)
		}
	}
	return f.BasenameStr
}

// ResolveFileContainerPath resolves the inode to a full path
func (ev *Event) ResolveFileContainerPath(f *model.FileEvent) string {
	if len(f.ContainerPath) == 0 {
		f.ContainerPath = ev.resolvers.resolveContainerPath(&f.FileFields)
	}
	return f.ContainerPath
}

// ResolveFileFilesystem resolves the filesystem a file resides in
func (ev *Event) ResolveFileFilesystem(f *model.FileEvent) string {
	return ev.resolvers.MountResolver.GetFilesystem(f.FileFields.MountID)
}

// ResolveFileInUpperLayer resolves whether the file is in an upper layer
func (ev *Event) ResolveFileInUpperLayer(f *model.FileEvent) bool {
	return f.FileFields.GetInUpperLayer()
}

// GetXAttrName returns the string representation of the extended attribute name
func (ev *Event) GetXAttrName(e *model.SetXAttrEvent) string {
	if len(e.Name) == 0 {
		e.Name = string(bytes.Trim(e.NameRaw[:], "\x00"))
	}
	return e.Name
}

// GetXAttrNamespace returns the string representation of the extended attribute namespace
func (ev *Event) GetXAttrNamespace(e *model.SetXAttrEvent) string {
	if len(e.Namespace) == 0 {
		fragments := strings.Split(ev.GetXAttrName(e), ".")
		if len(fragments) > 0 {
			e.Namespace = fragments[0]
		}
	}
	return e.Namespace
}

// ResolveMountPoint resolves the mountpoint to a full path
func (ev *Event) ResolveMountPoint(e *model.MountEvent) string {
	if len(e.MountPointStr) == 0 {
		e.MountPointStr, e.MountPointPathResolutionError = ev.resolvers.DentryResolver.Resolve(e.ParentMountID, e.ParentInode, 0)
	}
	return e.MountPointStr
}

// ResolveMountRoot resolves the mountpoint to a full path
func (ev *Event) ResolveMountRoot(e *model.MountEvent) string {
	if len(e.RootStr) == 0 {
		e.RootStr, e.RootPathResolutionError = ev.resolvers.DentryResolver.Resolve(e.RootMountID, e.RootInode, 0)
	}
	return e.RootStr
}

// ResolveContainerID resolves the container ID of the event
func (ev *Event) ResolveContainerID(e *model.ContainerContext) string {
	if len(e.ID) == 0 {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.ID = entry.ID
		}
	}
	return e.ID
}

// UnmarshalProcess unmarshal a Process
func (ev *Event) UnmarshalProcess(data []byte) (int, error) {
	// reset the process cache entry of the current event
	entry := NewProcessCacheEntry()
	entry.ContainerContext = ev.ContainerContext
	entry.ProcessContext = model.ProcessContext{
		Pid: ev.ProcessContext.Pid,
		Tid: ev.ProcessContext.Tid,
	}
	ev.processCacheEntry = entry

	n, err := ev.resolvers.ProcessResolver.unmarshalProcessCacheEntry(ev.processCacheEntry, data, false)
	if err != nil {
		return n, err
	}

	// Some fields need to be copied manually in the ExecEvent structure because they do not have "Exec" specific
	// resolvers, and the data was parsed in the ProcessCacheEntry structure. We couldn't introduce resolvers for these
	// fields because those resolvers would be shared with FileEvents.
	ev.Exec.FileFields = ev.processCacheEntry.ProcessContext.FileFields
	return n, nil
}

// ResolveUser resolves the user id of the file to a username
func (ev *Event) ResolveUser(e *model.FileFields) string {
	if len(e.User) == 0 {
		e.User, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.UID))
	}
	return e.User
}

// ResolveGroup resolves the group id of the file to a group name
func (ev *Event) ResolveGroup(e *model.FileFields) string {
	if len(e.Group) == 0 {
		e.Group, _ = ev.resolvers.UserGroupResolver.ResolveGroup(int(e.GID))
	}
	return e.Group
}

// ResolveChownUID resolves the user id of a chown event to a username
func (ev *Event) ResolveChownUID(e *model.ChownEvent) string {
	if len(e.User) == 0 {
		e.User, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.UID))
	}
	return e.User
}

// ResolveChownGID resolves the group id of a chown event to a group name
func (ev *Event) ResolveChownGID(e *model.ChownEvent) string {
	if len(e.Group) == 0 {
		e.Group, _ = ev.resolvers.UserGroupResolver.ResolveGroup(int(e.GID))
	}
	return e.Group
}

// ResolveProcessPPID resolves the parent process ID
func (ev *Event) ResolveProcessPPID(e *model.Process) int {
	if e.PPid == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.PPid = entry.PPid
		}
	}
	return int(e.PPid)
}

// ResolveProcessInode resolves the executable inode to a full path
func (ev *Event) ResolveProcessInode(e *model.Process) string {
	if len(e.PathnameStr) == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.PathnameStr = entry.PathnameStr
		}
	}
	return e.PathnameStr
}

// ResolveProcessContainerPath resolves the inode to a path relative to the container
func (ev *Event) ResolveProcessContainerPath(e *model.Process) string {
	if len(e.ContainerPath) == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.ContainerPath = entry.ContainerPath
		}
	}
	return e.ContainerPath
}

// ResolveProcessBasename resolves the inode to a filename
func (ev *Event) ResolveProcessBasename(e *model.Process) string {
	if len(e.BasenameStr) == 0 {
		if e.PathnameStr == "" {
			e.PathnameStr = ev.ResolveProcessInode(e)
		}

		e.BasenameStr = path.Base(e.PathnameStr)
	}
	return e.BasenameStr
}

// ResolveProcessCookie resolves the cookie of the process
func (ev *Event) ResolveProcessCookie(e *model.Process) int {
	if e.Cookie == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.Cookie = entry.Cookie
		}
	}
	return int(e.Cookie)
}

// ResolveProcessTTY resolves the name of the process tty
func (ev *Event) ResolveProcessTTY(e *model.Process) string {
	if e.TTYName == "" && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.TTYName = ev.resolvers.ProcessResolver.SetTTY(entry)
		}
	}
	return e.TTYName
}

// ResolveProcessFilesystem resolves the filesystem an executable resides in
func (ev *Event) ResolveProcessFilesystem(e *model.Process) string {
	if e.Filesystem == "" && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.Filesystem = ev.resolvers.MountResolver.GetFilesystem(entry.FileFields.MountID)
		}
	}
	return e.Filesystem
}

// ResolveProcessComm resolves the comm of the process
func (ev *Event) ResolveProcessComm(e *model.Process) string {
	if len(e.Comm) == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.Comm = entry.Comm
		}
	}
	return e.Comm
}

// ResolveExecArgs resolves the args of the event
func (ev *Event) ResolveExecArgs(e *model.ExecEvent) string {
	if ev.Exec.Args == "" && len(ev.ProcessContext.ArgsArray) > 0 {
		ev.Exec.Args = strings.Join(ev.ProcessContext.ArgsArray, " ")
	}
	return ev.Exec.Args
}

// ResolveExecEnvs resolves the envs of the event
func (ev *Event) ResolveExecEnvs(e *model.ExecEvent) []string {
	if len(ev.Exec.Envs) == 0 && len(ev.ProcessContext.EnvsArray) > 0 {
		ev.Exec.Envs = ev.ProcessContext.EnvsArray
	}
	return ev.Exec.Envs
}

// ResolveCredentialsUID resolves the user id of the process
func (ev *Event) ResolveCredentialsUID(e *model.Credentials) int {
	if e.UID == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.UID = entry.UID
		}
	}
	return int(e.UID)
}

// ResolveCredentialsUser resolves the user id of the process to a username
func (ev *Event) ResolveCredentialsUser(e *model.Credentials) string {
	if len(e.User) == 0 && ev != nil {
		e.User, _ = ev.resolvers.UserGroupResolver.ResolveUser(ev.ResolveCredentialsUID(e))
	}
	return e.User
}

// ResolveCredentialsGID resolves the group id of the process
func (ev *Event) ResolveCredentialsGID(e *model.Credentials) int {
	if e.GID == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.GID = entry.GID
		}
	}
	return int(e.GID)
}

// ResolveCredentialsGroup resolves the group id of the process to a group name
func (ev *Event) ResolveCredentialsGroup(e *model.Credentials) string {
	if len(e.Group) == 0 && ev != nil {
		e.Group, _ = ev.resolvers.UserGroupResolver.ResolveGroup(ev.ResolveCredentialsGID(e))
	}
	return e.Group
}

// ResolveCredentialsEUID resolves the effective user id of the process
func (ev *Event) ResolveCredentialsEUID(e *model.Credentials) int {
	if e.EUID == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.EUID = entry.EUID
		}
	}
	return int(e.EUID)
}

// ResolveCredentialsEUser resolves the effective user id of the process to a username
func (ev *Event) ResolveCredentialsEUser(e *model.Credentials) string {
	if len(e.EUser) == 0 && ev != nil {
		e.EUser, _ = ev.resolvers.UserGroupResolver.ResolveUser(ev.ResolveCredentialsEUID(e))
	}
	return e.EUser
}

// ResolveCredentialsEGID resolves the effective group id of the process
func (ev *Event) ResolveCredentialsEGID(e *model.Credentials) int {
	if e.EGID == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.EGID = entry.EGID
		}
	}
	return int(e.EGID)
}

// ResolveCredentialsEGroup resolves the effective group id of the process to a group name
func (ev *Event) ResolveCredentialsEGroup(e *model.Credentials) string {
	if len(e.EGroup) == 0 && ev != nil {
		e.EGroup, _ = ev.resolvers.UserGroupResolver.ResolveGroup(ev.ResolveCredentialsEGID(e))
	}
	return e.EGroup
}

// ResolveCredentialsFSUID resolves the file-system user id of the process
func (ev *Event) ResolveCredentialsFSUID(e *model.Credentials) int {
	if e.FSUID == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.FSUID = entry.FSUID
		}
	}
	return int(e.FSUID)
}

// ResolveCredentialsFSUser resolves the file-system user id of the process to a username
func (ev *Event) ResolveCredentialsFSUser(e *model.Credentials) string {
	if len(e.FSUser) == 0 && ev != nil {
		e.FSUser, _ = ev.resolvers.UserGroupResolver.ResolveUser(ev.ResolveCredentialsFSUID(e))
	}
	return e.FSUser
}

// ResolveCredentialsFSGID resolves the file-system group id of the process
func (ev *Event) ResolveCredentialsFSGID(e *model.Credentials) int {
	if e.FSGID == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.FSGID = entry.FSGID
		}
	}
	return int(e.FSGID)
}

// ResolveCredentialsFSGroup resolves the file-system group id of the process to a group name
func (ev *Event) ResolveCredentialsFSGroup(e *model.Credentials) string {
	if len(e.FSGroup) == 0 && ev != nil {
		e.FSGroup, _ = ev.resolvers.UserGroupResolver.ResolveGroup(ev.ResolveCredentialsFSGID(e))
	}
	return e.FSGroup
}

// ResolveCredentialsCapEffective resolves the cap_effective kernel capability of the process
func (ev *Event) ResolveCredentialsCapEffective(e *model.Credentials) int {
	if e.CapEffective == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.CapEffective = entry.CapEffective
		}
	}
	return int(e.CapEffective)
}

// ResolveCredentialsCapPermitted resolves the cap_permitted kernel capability of the process
func (ev *Event) ResolveCredentialsCapPermitted(e *model.Credentials) int {
	if e.CapPermitted == 0 && ev != nil {
		if entry := ev.ResolveProcessCacheEntry(); entry != nil {
			e.CapPermitted = entry.CapPermitted
		}
	}
	return int(e.CapPermitted)
}

// ResolveSetuidUser resolves the user of the Setuid event
func (ev *Event) ResolveSetuidUser(e *model.SetuidEvent) string {
	if len(e.User) == 0 && ev != nil {
		e.User, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.UID))
	}
	return e.User
}

// ResolveSetuidEUser resolves the effective user of the Setuid event
func (ev *Event) ResolveSetuidEUser(e *model.SetuidEvent) string {
	if len(e.EUser) == 0 && ev != nil {
		e.EUser, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.EUID))
	}
	return e.EUser
}

// ResolveSetuidFSUser resolves the file-system user of the Setuid event
func (ev *Event) ResolveSetuidFSUser(e *model.SetuidEvent) string {
	if len(e.FSUser) == 0 && ev != nil {
		e.FSUser, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.FSUID))
	}
	return e.FSUser
}

// ResolveSetgidGroup resolves the group of the Setgid event
func (ev *Event) ResolveSetgidGroup(e *model.SetgidEvent) string {
	if len(e.Group) == 0 && ev != nil {
		e.Group, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.GID))
	}
	return e.Group
}

// ResolveSetgidEGroup resolves the effective group of the Setgid event
func (ev *Event) ResolveSetgidEGroup(e *model.SetgidEvent) string {
	if len(e.EGroup) == 0 && ev != nil {
		e.EGroup, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.EGID))
	}
	return e.EGroup
}

// ResolveSetgidFSGroup resolves the file-system group of the Setgid event
func (ev *Event) ResolveSetgidFSGroup(e *model.SetgidEvent) string {
	if len(e.FSGroup) == 0 && ev != nil {
		e.FSGroup, _ = ev.resolvers.UserGroupResolver.ResolveUser(int(e.FSGID))
	}
	return e.FSGroup
}

// NewProcessCacheEntry returns an empty instance of ProcessCacheEntry
func NewProcessCacheEntry() *model.ProcessCacheEntry {
	return &model.ProcessCacheEntry{}
}

// ResolveProcessContextUser resolves the user id of the process to a username
func (ev *Event) ResolveProcessContextUser(p *model.ProcessContext) string {
	return ev.resolvers.ResolveProcessContextUser(p)
}

// ResolveProcessContextGroup resolves the group id of the process to a group name
func (ev *Event) ResolveProcessContextGroup(p *model.ProcessContext) string {
	return ev.resolvers.ResolveProcessContextGroup(p)
}

func (ev *Event) String() string {
	d, err := json.Marshal(ev)
	if err != nil {
		return err.Error()
	}
	return string(d)
}

// SetPathResolutionError sets the Event.pathResolutionError
func (ev *Event) SetPathResolutionError(err error) {
	ev.pathResolutionError = err
}

// MarshalJSON returns the JSON encoding of the event
func (ev *Event) MarshalJSON() ([]byte, error) {
	s := newEventSerializer(ev)
	return json.Marshal(s)
}

// ExtractEventInfo extracts cpu and timestamp from the raw data event
func ExtractEventInfo(data []byte) (uint64, uint64, error) {
	if len(data) < 16 {
		return 0, 0, model.ErrNotEnoughData
	}

	return model.ByteOrder.Uint64(data[0:8]), model.ByteOrder.Uint64(data[8:16]), nil
}

// ResolveEventTimestamp resolves the monolitic kernel event timestamp to an absolute time
func (ev *Event) ResolveEventTimestamp() time.Time {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = ev.resolvers.TimeResolver.ResolveMonotonicTimestamp(ev.TimestampRaw)
		if ev.Timestamp.IsZero() {
			ev.Timestamp = time.Now()
		}
	}
	return ev.Timestamp
}

func (ev *Event) setProcessContextWithProcessCacheEntry(entry *model.ProcessCacheEntry) {
	ev.ProcessContext.Ancestor = entry.Ancestor
	ev.ProcessContext.ArgsArray = entry.ArgsArray
	ev.ProcessContext.ArgsTruncated = entry.ArgsTruncated
	ev.ProcessContext.EnvsArray = entry.EnvsArray
	ev.ProcessContext.EnvsTruncated = entry.EnvsTruncated
}

// ResolveProcessCacheEntry queries the ProcessResolver to retrieve the ProcessCacheEntry of the event
func (ev *Event) ResolveProcessCacheEntry() *model.ProcessCacheEntry {
	if ev.processCacheEntry == nil {
		ev.processCacheEntry = ev.resolvers.ProcessResolver.Resolve(ev.ProcessContext.Pid, ev.ProcessContext.Tid)
		if ev.processCacheEntry == nil {
			ev.processCacheEntry = &model.ProcessCacheEntry{}
		}
	}

	ev.setProcessContextWithProcessCacheEntry(ev.processCacheEntry)

	return ev.processCacheEntry
}

// updateProcessCachePointer updates the internal pointers of the event structure to the ProcessCacheEntry of the event
func (ev *Event) updateProcessCachePointer(entry *model.ProcessCacheEntry) {
	ev.setProcessContextWithProcessCacheEntry(entry)

	ev.processCacheEntry = entry
}

// Clone returns a copy on the event
func (ev *Event) Clone() Event {
	return *ev
}

// NewEvent returns a new event
func NewEvent(resolvers *Resolvers, scrubber *pconfig.DataScrubber) *Event {
	return &Event{
		Event:     model.Event{},
		resolvers: resolvers,
		scrubber:  scrubber,
	}
}
