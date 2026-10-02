package fmsworker

import "time"

// Wire types for the schema-2 disk requests (contract: worker-ipc, disk-sharing).
// Requests are flat JSON objects; the worker matches fields by name, so these
// names must stay equal to ipc.Request in the FMS worker. Responses are decoded
// by wireResponse in client.go.

// BaseRequest carries the envelope fields shared by every disk request.
type BaseRequest struct {
	RequestID     string    `json:"requestId"`
	SchemaVersion int       `json:"schemaVersion"`
	Type          string    `json:"type"`
	Action        string    `json:"action"`
	Timestamp     time.Time `json:"timestamp"`
}

// DiskShareRequest requests sharing or unsharing of a disk.
type DiskShareRequest struct {
	BaseRequest
	ContainerPath string `json:"containerPath"`
	RootName      string `json:"rootName,omitempty"`
	ReadOnly      bool   `json:"readOnly"`
	Owner         string `json:"owner,omitempty"`
	Persist       bool   `json:"persist"`
}

// DiskOpenRequest requests opening (mounting) of a shared disk.
type DiskOpenRequest struct {
	BaseRequest
	ContainerPath string           `json:"containerPath"`
	Password      string           `json:"password,omitempty"`
	MountOptions  DiskMountOptions `json:"mountOptions,omitempty"`
	Owner         string           `json:"owner,omitempty"`
}

// DiskMountOptions contains options for mounting a disk.
type DiskMountOptions struct {
	NoLetter   bool   `json:"noLetter"`
	FolderPath string `json:"folderPath,omitempty"`
	ReadOnly   bool   `json:"readOnly"`
	VolumeGUID string `json:"volumeGuid,omitempty"`
}

// DiskCloseRequest requests closing (unmounting) of a shared disk.
type DiskCloseRequest struct {
	BaseRequest
	ContainerPath string `json:"containerPath"`
	Force         bool   `json:"force"`
	// DrainTimeout is the maximum time to wait for handles to close, in seconds.
	DrainTimeout int    `json:"drainTimeout"`
	Owner        string `json:"owner,omitempty"`
}

// DiskAutostartRequest requests setting or clearing the autostart mark.
type DiskAutostartRequest struct {
	BaseRequest
	ContainerPath string `json:"containerPath"`
	Enable        bool   `json:"enable"`
	Consent       bool   `json:"consent"`
	// Password is the credential to store for an encrypted disk; it is never logged.
	Password string `json:"password,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

// DiskQueryRequest is the status request (ContainerPath set) and the list request.
type DiskQueryRequest struct {
	BaseRequest
	ContainerPath string `json:"containerPath,omitempty"`
	Owner         string `json:"owner"`
}
