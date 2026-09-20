# FDSEC-FORMAT - the FileDO secret-file container, format version 1

Status: published contract. This document defines the on-disk format completely enough that a
third party can write a reader from it alone, without any FileDO source code. It is published
**before** any implementation ships: the format is a contract, not a side effect of an
implementation, so no FileDO release writes this container yet. Amendments follow the rules in
section 13 and are recorded in section 17.

Suite 1 is defined here in full. **Suite 2 is reserved** for a private construction that is
deliberately not published; see section 13.3. Nothing in this document depends on suite 2, and
suite-2 files depend on nothing in this document except the dispatch rule of section 13.4.

Terminology, fixed once and used without exception: a container made under a **non-empty**
credential is *encrypted*. A container made under an **empty** credential is *obfuscation* -
it hides a file from a casual look and provides **no secrecy**. No surface of this format may
call an empty credential protection.

## 1. What the format is

A secret-file container is one ordinary file holding exactly one original file - its bytes, its
true name, its real size and its timestamps - behind a credential. The visible name of a
container is the original's name with the original extension dropped and `.fd-sec` appended:
`video.mp4` becomes `video.fd-sec`. The container can be copied, mailed and backed up like any
file. It is never updated in place: every write produces a whole new file.

Primitives, all standard with published test vectors:

| Role | Primitive |
| --- | --- |
| Key derivation, slow branch | Argon2id (`golang.org/x/crypto/argon2`, RFC 9106) |
| Key derivation, fast branch | BLAKE2b-512 (`golang.org/x/crypto/blake2b`, RFC 7693) |
| AEAD (wrap, metadata, chunks) | XChaCha20-Poly1305 (`golang.org/x/crypto/chacha20poly1305`, draft-irtf-cfrg-xchacha) |
| Digests | BLAKE2b-256 and BLAKE2b-512 |
| Randomness | the operating system CSPRNG (`crypto/rand`) |

## 2. Conventions

- All integers are unsigned, **little-endian**.
- All byte offsets are zero-based from the start of the file.
- Strings are UTF-8. Credentials and names are normalised to Unicode NFC before use; no
  whitespace is trimmed - a leading or trailing space is part of the credential.
- `u8`, `u16le`, `u32le`, `u64le` are unsigned integers of 1, 2, 4, 8 bytes.
- `BLAKE2b-256(x)` is the 32-byte BLAKE2b digest of `x`; `BLAKE2b-512(x)` the 64-byte digest.
  `BLAKE2b-256(key = k, msg = m)` is keyed BLAKE2b with a 32-byte key.
- `Seal(k, nonce, ad, pt)` / `Open(k, nonce, ad, ct)` are XChaCha20-Poly1305 with a 32-byte key,
  a 24-byte nonce, associated data `ad`, and a 16-byte tag appended to the ciphertext.
- `a || b` is byte concatenation.

## 3. File map

A framed (suite-1) container, in the large:

```
+-- clear header, 81 bytes ----------------------------------------------+
| magic | version | suite | flags | salt | KDF params | threshold |      |
| chunk size | cluster alignment | header digest                        |
+-- key slots, 8 records of 80 bytes = 640 bytes ------------------------+
| slot 0: the file key wrapped under the credential-derived key          |
| slots 1..7: reserved, typed, empty in version 1                        |
+-- sealed metadata, 4112 bytes -----------------------------------------+
| name, real size, timestamps, digest of the original - all encrypted    |
+-- alignment pad -------------------------------------------------------+
| random bytes so the whole pre-payload region is cluster-aligned        |
+-- payload -------------------------------------------------------------+
| chunk 0 .. chunk k-1, each individually authenticated, each cluster-   |
| aligned, the last one short and flagged, then a random tail pad        |
+-----------------------------------------------------------------------+
```

With the default parameters (`cluster alignment` A = 4096, `chunk size` S = 1048576):

| Region | Offset | Length |
| --- | --- | --- |
| clear header | 0 | 81 |
| key slots | 81 | 640 |
| sealed metadata | 721 | 4112 |
| alignment pad | 4833 | 3359 (to offset 8192) |
| chunk 0 | 8192 | S |
| further full chunks | 8192 + i*S | S each |
| last chunk | 8192 + (k-1)*S | n + 16 |
| tail pad | rest | 0..A-1, to a cluster boundary |

The pre-payload length `pre_len = 8192` under the defaults; in general
`pre_len = ceil((81 + 640 + 4112) / A) * A`.

## 4. The clear header (81 bytes)

| Offset | Size | Field | Value and meaning |
| --- | --- | --- | --- |
| 0 | 5 | magic | ASCII `FDSEC` (0x46 0x44 0x53 0x45 0x43) |
| 5 | 1 | format version | 1 |
| 6 | 1 | suite id | 1 for this suite; see 13.3 |
| 7 | 2 | flags (u16le) | bit 0 (0x0001) is the conformance bit and must be set; bits 1..15 are reserved and must be zero |
| 9 | 16 | salt | CSPRNG bytes; feeds the key derivation and the fast-branch hash |
| 25 | 4 | KDF memory M_KiB (u32le) | Argon2id memory in KiB |
| 29 | 4 | KDF iterations T (u32le) | Argon2id time cost |
| 33 | 1 | KDF lanes P (u8) | Argon2id parallelism, 1..255 |
| 34 | 1 | KDF key length (u8) | must be 32 |
| 35 | 2 | KDF reserved (u16le) | must be zero |
| 37 | 4 | KDF length threshold (u32le) | credential length in UTF-8 bytes at which the fast branch takes over; must be at least 1 |
| 41 | 4 | chunk size S (u32le) | on-disk size of a full chunk slot; the plaintext capacity of a full slot is S - 16 |
| 45 | 4 | cluster alignment A (u32le) | power of two, 512..2097152 |
| 49 | 32 | header digest | BLAKE2b-256 over bytes [0, 49) of the file |

A reader that finds the magic parses the header and verifies the header digest first. A bad
header digest means the file is **damaged** - never a wrong credential. An unknown format
version, or any set flag bit other than 0x0001, is refused as an unsupported container, not
reported as damage: a future field activated in those bits must be refused by old readers, not
mis-parsed by them (rule 13.2).

## 5. Key slots (8 records of 80 bytes, at offset 81)

Each record:

| Offset in record | Size | Field |
| --- | --- | --- |
| 0 | 1 | slot type (u8) |
| 1 | 79 | payload, defined per type |

Slot types:

| Type | Meaning | Payload layout |
| --- | --- | --- |
| 0 | empty | 79 zero bytes |
| 1 | credential-wrapped file key | wrap_nonce (24) `||` wrapped (48) `||` 6 zero bytes |
| 2 | **reserved by name**: recovery-keyfile-wrapped file key (not written by any current writer) | not yet defined |
| 3..255 | unallocated | - |

In version 1, slot 0 holds type 1 and slots 1..7 must hold type 0. A reader that meets an
unknown or reserved type in a slot it needs refuses it as an unsupported key-slot type; this
refusal is what lets type 2 be activated later without a format version bump (rule 13.2).

**Slot 0, the credential wrap.** The writer generates a fresh 32-byte **file key** from the
CSPRNG; the payload is never encrypted under the credential directly. Let `KEK` be the
credential-derived key of section 6. Then:

```
wrapped = Seal(KEK, wrap_nonce, "FDSEC1/slot0" || header_digest, file_key)
```

`wrapped` is 48 bytes (32-byte key plus 16-byte tag). Unwrapping either authenticates or it
does not; failure is reported as a **wrong credential** (or, indistinguishably, tampering with
the slot) - never as damage.

The wrap-not-encrypt rule is what makes a future password change a rewrite of a few hundred
bytes instead of the whole file, and what makes the reserved recovery slot of type 2 a format
feature rather than a redesign.

## 6. The credential

A credential is a text string of any length, including zero. It is normalised (NFC, no
trimming) and its UTF-8 byte length `len` is compared with the header's threshold `threshold`.

- **Slow branch, `len < threshold`** (short passwords get a deliberately expensive derivation):

  ```
  KEK = Argon2id(password = credential_utf8, salt = salt, time = T,
                 memory = M_KiB KiB, threads = P, keyLen = 32)
  ```

- **Fast branch, `len >= threshold`** (a long credential is trusted to carry its own entropy,
  and only a salted hash separates it from the key):

  ```
  KEK = BLAKE2b-512(salt || credential_utf8)[0:32]
  ```

The branch is recomputed by the reader from the length of the credential it holds and the
threshold in the header; which branch was used is stored nowhere. The threshold must be at
least 1, so an empty credential always takes the slow branch.

Writer defaults for version 1: `threshold` = 64, `M_KiB` = 65536 (64 MiB), `T` = 3, `P` = 4.
They are per-file header fields: a reader obeys the file, never its own defaults.

Honest statements this section requires of every surface built on it. Length is not entropy -
sixty-four repetitions of one character take the fast branch. An empty credential is accepted
and produces a container that is **obfuscation with no secrecy**, and must never be described
as protected. There is no recovery from a forgotten credential: no verifier, no escrow, no
back door exists in this format (the reserved slot of type 2 is the one mitigation the format
allows, and version 1 does not implement it).

## 7. Sealed metadata (at offset 721, 4112 bytes on disk)

The plaintext metadata block is exactly 4096 bytes:

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 2 | name length (u16le), 1..3000 |
| 2 | name length | true name of the original, UTF-8, NFC, no path components |
| 2 + name length | 8 | real size N of the original in bytes (u64le) |
| +8 | 8 | time of encryption (u64le, Windows FILETIME, UTC) |
| +8 | 8 | original creation time (u64le, FILETIME) |
| +8 | 8 | original last-access time (u64le, FILETIME) |
| +8 | 8 | original last-write time (u64le, FILETIME) |
| +8 | 32 | D = BLAKE2b-256 of the original's bytes |
| rest | to 4096 | random padding (hides the name length; a fixed block hides it completely) |

Sealing (section 8 defines `meta_nonce`):

```
sealed_metadata = Seal(file_key, meta_nonce, "FDSEC1/meta" || header_digest, metadata_block)
```

4112 bytes on disk (4096 + 16 tag). Everything an outside observer could want - the true name,
the real size, the timestamps, the digest - is inside this encrypted block; the container's
visible name and its own timestamps are the only facts that leak (section 14).

The digest D is the read-back proof: after unpacking, the recovered bytes must hash to D. No
implementation may delete, move or overwrite an original until its container has been reopened
and read back to this digest.

A writer refuses an original whose name exceeds 3000 UTF-8 bytes rather than truncating it.

## 8. Payload chunks

Payload starts at `pre_len`. Let:

- `P = S - 16` - the plaintext capacity of a full chunk slot (the 16 bytes are the tag);
- `k = max(1, ceil(N / P))` - the number of chunks (an empty original has one chunk of length 0);
- `n_i` - the plaintext length of chunk i: P for every chunk except the last,
  `n = N - P * (k - 1)` for the last (0 <= n <= P).

Chunk i occupies `[pre_len + i*S, pre_len + i*S + len_i + 16)` where `len_i` is P for a full
chunk and n for the last. After the last chunk's tag the file is padded with random bytes so
the total file length is a multiple of A (0 to A-1 bytes of tail pad).

**Nonce derivation.** Chunk and metadata nonces are derived from the file key, not stored
(anything stored would either break the cluster alignment of section 9 or add a header field
that does not exist):

```
nonce(ctx) = BLAKE2b-256(key = file_key, msg = "FDSEC1/nonce/" || ctx)[0:24]

meta_nonce  with ctx = "meta"
chunk_nonce(i) with ctx = "chunk:" || u64le(i)
```

The file key is fresh and random for every container, so nonces derived from it are unique per
file and per chunk index with no counter bookkeeping.

**Chunk sealing.** Each chunk is sealed on its own, bound to its index, to the chunk count, to
the header, to its length, and to its last-or-not position:

```
ad_i = "FDSEC1/chunk" || header_digest || u64le(i) || u64le(k) || u8(last) || u64le(len_i)
chunk_i = Seal(file_key, chunk_nonce(i), ad_i, plaintext_i)
```

`last` is 1 for i = k-1 and 0 otherwise.

What each binding buys, each being a failure that must be impossible rather than unlikely:

- the index in the AD makes **reordering** detectable (a swapped chunk fails its tag);
- the header digest in the AD makes **splicing between containers** detectable;
- the last flag and the length make **truncation** detectable - dropping or shortening the
  final chunk breaks the arithmetic of section 12 step 8, and a chunk falsely presented as
  last fails its tag;
- per-chunk authentication makes **substitution of any block** detectable at that chunk,
  without reading the rest of the file;
- chunking means a 40 GB original never needs 40 GB of memory, and a future seek-based reader
  can decrypt one chunk at a time.

The whole payload is padded to a cluster boundary; the real size N lives in the sealed
metadata, so the padding costs no fidelity and hides the exact figure (section 14).

## 9. Alignment invariants

These are rules of the format, not choices of a writer. A file that breaks any of them is
structurally impossible and is reported as damaged:

1. `S` is a multiple of `A`, and `S >= A`.
2. `pre_len` is a multiple of `A`.
3. The total file length is a multiple of `A`.
4. Therefore every chunk boundary is also a cluster boundary.

The reason is permanent: if `S` were not a multiple of `A`, every chunk would straddle a
cluster edge, and any future in-place or seek-based operation would turn every chunk touch
into a read-modify-write of the neighbouring cluster - on every file ever written, forever.

## 10. Writer algorithm

1. Normalise the credential (section 6). Generate the salt and the file key from the CSPRNG;
   generate the wrap nonce for slot 0.
2. Compute D = BLAKE2b-256 over the original's bytes while streaming it once.
3. Build the metadata block, seal it, build the header and the header digest, wrap the file
   key into slot 0.
4. Write to a temporary name in the destination directory: header, slots, sealed metadata,
   alignment pad, then chunk 0..k-1 sealed in order, then the tail pad.
5. Flush, reopen the temporary file, and read it back: unwrap the key from what was written,
   open the metadata, verify every chunk, and hash the recovered plaintext to D.
6. Rename into place (`<name>.fd-sec`, or a random name without the extension when the user
   asked for a nameless blob). Only after a successful read-back may any disposition of the
   original (keep, delete, wipe) be carried out, and only when the user asked.

An interrupted pack therefore leaves either nothing or a partial temporary file - never a
container that looks complete. An implementation must clean up its own temporary file on
Ctrl+C and on failure.

## 11. Reader algorithm

1. Read bytes [0, 5). `FDSEC` -> framed path, continue. Anything else -> this is not a framed
   container; a FileDO reader falls back to the quiet suite-2 open (13.4). A third-party
   suite-1 reader reports "not a suite-1 container".
2. Parse the header. Verify the header digest over [0, 49). Failure -> damaged.
3. Version must be 1; suite id must be 1; flags must be exactly 0x0001; KDF key length must be
   32; A must be a power of two in [512, 2097152]; S a multiple of A; threshold at least 1.
   Any other value -> unsupported container or damaged as stated in section 4.
4. Derive KEK from the credential, the salt and the threshold branch (section 6).
5. Unwrap slot 0. Tag failure -> wrong credential (or the slot was tampered with - the two are
   indistinguishable and the message must not choose between them).
6. Open the sealed metadata. Tag failure -> wrong credential or tampering. Parse the name, N,
   the timestamps and D.
7. Compute P, k, n from S and N. Compute the expected minimal length
   `L_min = pre_len + (k-1)*S + n + 16`. The file must be at least L_min, less than L_min + A,
   and its length must be a multiple of A. Anything else -> damaged (truncated, or padded
   beyond one cluster).
8. Open chunk 0..k-1 in order at their fixed offsets, each with its own derived nonce and AD.
   Any tag failure -> tampering (or corruption - indistinguishable, and said so).
9. Stream the recovered plaintext out; at the end, BLAKE2b-256 of the recovered bytes must
   equal D. Mismatch -> damaged, with the recovered bytes discarded.
10. Restore the true name, the real size and the original timestamps from the metadata.

## 12. Damage taxonomy

Three outcomes are distinct and must never be reported as each other:

| Outcome | Produced by |
| --- | --- |
| wrong credential (or tampering) | slot-0 unwrap tag failure; metadata or chunk tag failure on a structurally intact file |
| damaged | bad header digest; structural rule broken; length arithmetic broken (truncation); final digest D mismatch |
| unsupported | unknown version, suite, flag bit or slot type - refused by name, never called damage |

Cryptographically, a wrong key and a flipped byte both fail an AEAD tag identically; that is a
security property, and a message must never claim "wrong password" for what might be damage,
nor "damaged" for what might be a wrong password.

## 13. Version, suite and extension rules

### 13.1 Format version

Version is 1. It is bumped only when the meaning of a framed field changes for existing
suites. A version bump obliges a reader for every earlier version, forever: a change that
would make an existing container unreadable requires the bump and the back-reader, or it does
not happen. This rule outranks every convenience.

### 13.2 Adding fields

- A new suite (13.3) may define any internal construction it likes; it is not a version bump
  and touches no existing reader. This is the primary extension mechanism.
- A reserved field (a flag bit, a slot type) may be brought into use **without** a version bump
  if and only if version-1 readers already refuse it as unsupported rather than mis-parsing it.
  That is why unknown flag bits are refused and empty slots are typed, not blank.

### 13.3 Suite ids

| Suite id | Status |
| --- | --- |
| 0 | invalid |
| 1 | the published AEAD suite, defined by this document |
| 2 | reserved for a private, hardened construction. Its construction is intentionally not published. It is the **quiet** suite: frameless, uniform noise from byte 0, no magic, no version, no suite field on disk. A framed file claiming suite id 2 is invalid - suite 2 never carries this frame |
| 3..255 | unallocated; allocated only by an amendment to this document |

### 13.4 Quiet dispatch

A FileDO reader distinguishes framed from quiet by parsing: it looks for the magic `FDSEC`
first, and on absence falls back to the quiet suite-2 open. Consequence, accepted deliberately:
nothing on the disk says what a nameless quiet blob is - a container whose visible name has
been stripped of `.fd-sec` (or was written under a random name) is opened by explicit command,
not by double-click, and `fdsec info` can say nothing about it without the credential.

## 14. What leaks, and what does not

Honest list, because a secrecy format that overstates itself is worse than none:

- The **approximate size** of the original - cluster-padded payload and a random tail pad
  leave the exact figure invisible; the real one is inside the sealed metadata.
- That this is a FileDO secret file - from the `.fd-sec` extension and the magic bytes. A
  renamed quiet suite-2 blob leaks not even this; a framed suite-1 container always does.
- The container's own name and location, hence roughly what the original was called - but not
  what kind of file it was, since the visible name carries no original extension.
- The container's own timestamps, which are the time of packing.
- Nothing about the original's content, its type, or the length of the credential.

## 15. What version 1 deliberately does not carry

Compression (a size side channel when combined with encryption), multi-file and folder
containers (a set belongs to a different container family, not to a pile of `.fd-sec` files),
split containers, editing a container in place, and a second filled key slot. The recovery slot
is reserved (section 5); implementing it is a separate decision, not a format change.

## 16. Test-vector contract

Every implementation of suite 1 must be checkable by a third party against committed test
vectors. The vectors fix every random input (salt, file key, wrap nonce, metadata padding,
tail padding, and the original's bytes) and publish, in hexadecimal, field by field:

1. both KEK branches: one credential below the threshold (Argon2id with the file's M, T, P)
   and one at or above it (the salted BLAKE2b-512 branch), each with its expected 32-byte KEK;
2. the 81-byte header with its header digest;
3. the wrapped slot 0 (wrap nonce and wrapped bytes);
4. the sealed metadata block;
5. each sealed chunk of a multi-chunk original, with its AD spelled out;
6. two complete containers - an empty original (N = 0) and a one-byte original - with the
   BLAKE2b-256 file digest of each.

A vector file names its inputs and outputs field by field so a third party can implement from
this document, run the vectors, and compare byte for byte.

## 17. Document log

| Date | Change |
| --- | --- |
| 2026-09-20 | Published: format version 1, suite 1 defined in full, suite 2 reserved (quiet), key-slot region and flag bits reserved, alignment invariants fixed, test-vector contract stated. No writer ships yet. |
