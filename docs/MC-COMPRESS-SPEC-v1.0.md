# MC-COMPRESS-SPEC v1.0

**Memcached Protocol-Level Compression Flag Negotiation Specification**

---

## 1. Overview

This specification defines a standardized bit allocation scheme for the 32-bit `flags` field in the Memcached protocol. It enables any compliant client to unambiguously identify whether a stored value is compressed, which algorithm was used, and how to decode the flags — achieving true cross-language, cross-framework interoperability.

The core mechanism is a **MAGIC field** in the highest 4 bits. A compliant client checks the MAGIC first: if it matches `0xA`, the flags follow this spec; otherwise, the data comes from a legacy client and is handled via fallback rules.

---

## 2. MC-FLAGS Bit Layout

```
  31  28 27 24 23                                    8 7     0
  ┌─────┬────┬──────────────────────────────────────────┬───────┐
  │MAGIC│CAR │              APP-FLAGS                   │RSRVD  │
  │ 0xA │-ID │              (16 bits)                   │(8bit) │
  └─────┴────┴──────────────────────────────────────────┴───────┘
```

| Bits   | Width | Name       | Description                                                       |
|--------|-------|------------|-------------------------------------------------------------------|
| 31–28  | 4     | MAGIC      | Fixed value `0xA`. If ≠ `0xA`, flags are not from this spec.      |
| 27–24  | 4     | CAR-ID     | Compression algorithm ID. `0x0` = uncompressed, `0x1`–`0xF` = algorithm. |
| 23–8   | 16    | APP-FLAGS  | Application-defined flags. Free for business use; this spec does not define semantics. |
| 7–0    | 8     | RESERVED   | Must be `0`. Reserved for future use.                             |

### Encoding Formula

```c
flags = ((MAGIC      & 0xF)    << 28)
      | ((CAR_ID     & 0xF)    << 24)
      | ((APP_FLAGS  & 0xFFFF) << 8)
      |  (RESERVED   & 0xFF);
```

### Decoding Formula

```c
magic     = (flags >> 28) & 0xF;
car_id    = (flags >> 24) & 0xF;
app_flags = (flags >> 8)  & 0xFFFF;
reserved  =  flags        & 0xFF;
```

### Example

ZSTD-compressed value, `APP-FLAGS = 0x0000`:

```
MAGIC=0xA, CAR-ID=0x5, APP-FLAGS=0x0000, RESERVED=0x00
→ flags = 0xA5000000 (decimal 2,770,538,496)
```

---

## 3. MAGIC Field

**Value:** `0xA` (binary `1010`)

**Purpose:** Deterministic spec identification. A single comparison replaces heuristic guessing.

**Why 0xA?**
- Binary pattern `1010` is highly distinctive vs. all-zero legacy flags.
- Hex `0xA...` is immediately recognizable in any hex dump.
- Legacy clients use small integers (0, 1, 2, 16, …) where bits 31–28 are always `0x0` — zero collision risk.
- 4-bit space gives 1/16 random collision probability; actual collision probability is near-zero given real flags distributions.

**MUST:** Compliant clients set MAGIC = `0xA` on every write.

---

## 4. CAR-ID Field

**4 bits**, values `0x0`–`0xF`. Dual semantics:

| CAR-ID | Meaning        |
|--------|----------------|
| `0x0`  | Uncompressed   |
| `0x1+` | Compressed; value identifies the algorithm per the CAR (Section 5) |

**Why 4 bits?**
- 8 current algorithms + 8 reserved = 16 slots, sufficient for the foreseeable future.
- If exceeded, RESERVED bit 7 can extend CAR-ID to 5 bits (32 algorithms) without changing existing assignments.

---

## 5. Compression Algorithm Registry (CAR)

| CAR-ID      | Name     | Default Level | Notes                             |
|-------------|----------|---------------|-----------------------------------|
| `0x0`       | NONE     | N/A           | No compression                    |
| `0x1`       | DEFLATE  | 6             | zlib format (RFC 1950)            |
| `0x2`       | LZ4      | 1             | LZ4 Block format; low latency     |
| `0x3`       | LZ4HC    | 9             | LZ4 high-compression variant      |
| `0x4`       | SNAPPY   | default       | High throughput                   |
| `0x5`       | ZSTD     | 3             | Balanced ratio & speed            |
| `0x6`       | LZMA     | 6             | Maximum ratio, slow compress      |
| `0x7`       | BROTLI   | 4             | Better than DEFLATE for web data  |
| `0x8`–`0xF` | RESERVED | N/A           | For future algorithm registration |

**MUST** support decompression: DEFLATE (`0x1`).
**SHOULD** support decompression: LZ4 (`0x2`), ZSTD (`0x5`).

Compression level is NOT encoded in flags — it is transparent to the decompressor and left to the writer's discretion.

---

## 6. APP-FLAGS Field

**16 bits** (bit 23–8), entirely application-defined.

- This spec imposes **no semantics** on APP-FLAGS.
- Compliant clients **MUST NOT** modify or overwrite APP-FLAGS set by the caller.
- If the caller provides no value, the client **SHOULD** set APP-FLAGS to `0x0000`.
- Typical uses: data source marker, LRU eviction policy, priority, async refresh flag, etc.

---

## 7. Client Behavior

### 7.1 Write Flow (SET/ADD/REPLACE)

| Step | Action                                                                        | Level |
|------|-------------------------------------------------------------------------------|-------|
| 1    | Set MAGIC = `0xA`                                                             | MUST  |
| 2    | Check if `len(value) ≥ compression_threshold` (default 1024 bytes)            | MUST  |
| 3a   | Below threshold → CAR-ID = `0x0`, store raw value                             | MUST  |
| 3b   | At/above threshold → compress. If compressed size ≥ original, fall back to 3a | MUST  |
| 3c   | Compression succeeded → CAR-ID = algorithm's CAR-ID (`0x1`–`0xF`)             | MUST  |
| 4    | Preserve caller's APP-FLAGS; do not overwrite                                 | MUST  |
| 5    | Set RESERVED = `0x00`                                                         | MUST  |
| 6    | Encode flags per the formula and issue the memcached command                  | MUST  |

### 7.2 Read Flow (GET/GETS)

| Step | Action                                                                            | Level  |
|------|-----------------------------------------------------------------------------------|--------|
| 1    | Retrieve `flags` and `value` from memcached                                       | MUST   |
| 2a   | Extract MAGIC = `(flags >> 28) & 0xF`. If = `0xA` → proceed to step 3             | MUST   |
| 2b   | If MAGIC ≠ `0xA` → handle as legacy data (Section 9)                              | MUST   |
| 3a   | Extract CAR-ID = `(flags >> 24) & 0xF`. If = `0x0` → value is uncompressed        | MUST   |
| 3b   | If CAR-ID is a known algorithm → decompress value                                 | MUST   |
| 3c   | If CAR-ID is unknown → execute fallback (Section 8.2)                             | MUST   |
| 4    | Extract APP-FLAGS = `(flags >> 8) & 0xFFFF`; return value with metadata to caller | MUST   |

---

## 8. Fallback & Error Handling

### 8.1 MAGIC Mismatch

When MAGIC ≠ `0xA`, the flags were NOT generated by this spec.

| Priority | Strategy                                            |
|----------|-----------------------------------------------------|
| 1        | Try to identify legacy flags convention (Section 9) |
| 2        | Return raw value with `magic_mismatch = true`       |
| 3        | Log WARNING and return MISS                         |

### 8.2 Unknown CAR-ID

MAGIC = `0xA` but CAR-ID is not in the local algorithm list.

| Priority | Strategy                                                               |
|----------|------------------------------------------------------------------------|
| 1        | Try to dynamically load the algorithm library                          |
| 2        | Return raw compressed value with `is_compressed = true` and the CAR-ID |
| 3        | Log ERROR and return MISS                                              |

### 8.3 Decompression Failure

Known CAR-ID but decompression fails (corrupt data, checksum mismatch, etc.):

- **MUST** log ERROR with key, CAR-ID, error type.
- **MUST** return MISS (not an exception).
- **MUST NOT** return partially decompressed or corrupt data.
- **SHOULD** increment a `decompress_error` metric counter.

### 8.4 Compression Expands Data

If compressed size ≥ original size:

- **MUST** store the raw value with CAR-ID = `0x0`.
- **MAY** skip compression if savings < 10% (configurable).

---

## 9. Legacy Client Compatibility

### 9.1 Coexistence Strategy

Compliant and legacy clients can safely share a Memcached cluster:

- **Deterministic identification:** MAGIC = `0xA` guarantees no ambiguity — legacy flags always have high bits = `0x0`.
- **Write isolation:** Use key prefix conventions (e.g., `mc:` for compliant clients) to prevent cross-reads.
- **Gradual migration:** Deploy compliant read support first → switch writes to MC-FLAGS format → retire legacy clients.

### 9.2 Legacy Flags Recognition

When MAGIC ≠ `0xA`:

| Condition                                   | Interpretation                  | Action                        |
|---------------------------------------------|---------------------------------|-------------------------------|
| `flags == 0`                                | Legacy, uncompressed            | Use value as-is               |
| `flags == 1` (bit 0 only)                   | Legacy, zlib-compressed (PHP)   | Try zlib decompress           |
| `flags == 2` (bit 1 only)                   | Legacy, zlib-compressed (Enyim) | Try zlib decompress           |
| `flags & 0xFF00 != 0` and low byte = 0      | Legacy, high-byte = type marker | Use value as-is               |
| Other                                       | Unrecognized                    | Return raw value with warning |

---

## 10. Security Considerations

### 10.1 Zip Bombs

- **SHOULD** enforce a decompressed size limit (default: min(100× compressed size, 1 GB)).
- **SHOULD** use streaming decompression with size monitoring and early termination.

### 10.2 Data Integrity

- No checksum field exists in MC-FLAGS v1.0.
- Value integrity relies on Memcached storage reliability and TCP checksums.
- Future spec versions may allocate RESERVED bits for a lightweight CRC.

### 10.3 Algorithm Downgrade

- Replacing a value with a different compression algorithm while keeping the same CAR-ID will cause decompression failure (different format), not data confusion.
- Clients return MISS on decompression failure — damage is limited to cache miss, not data corruption.

---

## Appendix: Reserved Bits Extension Roadmap

RESERVED (bit 7–0) is available for future extensions:

| Possible Use        | Bits Needed | Notes                                          |
|---------------------|-------------|------------------------------------------------|
| CAR-ID expansion    | 1 (bit 7)   | Extends CAR-ID from 4→5 bits (32 algorithms)   |
| Compression level   | 3–4         | Optional fine-grained level hint               |
| Integrity check     | 4–8         | CRC4/CRC8 for lightweight data verification    |

These extensions will be defined in future spec versions with full backward compatibility.
