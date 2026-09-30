// Package vdisk is the FileDO disk container: one ordinary .fdd file that
// holds the sectors of one block device, in the format of the catalog contract
// FDD-FORMAT (wire version 1.0) and with the behaviour of FDD-BEHAVIOUR.
//
// The layers, each knowing nothing of the one above it: Container (logical
// volume offsets) -> the cluster map -> the AES-256-XTS sector layer, whose
// tweak is the logical sector index -> the backing file.
//
// Every container passes its data through XTS. When the data key is wrapped
// under a key derived from the published pepper alone - every container this
// build creates - that layer is obfuscation: it hides the volume from a casual
// look and from disk-image scanners, and it protects nothing from anyone who
// holds the file and the format. Only a container whose data key sits behind
// a credential is encrypted, and this build does not create or open one.
//
// The read path - Inspect, Verify, ExportRaw - needs no mount, no driver, no
// socket and no elevation, and a read-only open writes nothing to the file.
package vdisk
