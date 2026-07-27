# Streamed Statement CSV Export

## Overview

The statement listing endpoint (`GET /api/v1/statements`) supports direct HTTP streaming of statement records in CSV format via HTTP content negotiation. When clients include `Accept: text/csv` in their request headers, the server formats and streams statement rows immediately over the connection instead of returning JSON or requiring an intermediate asynchronous S3 export job.

This feature enables seamless spreadsheet workflows for tenant operators, automated financial reconciliation scripts, and direct Excel/LibreOffice imports.

---

## Endpoint Specification

```http
GET /api/v1/statements?customer_id={customerID}&limit={limit}
Accept: text/csv
Authorization: Bearer <jwt_token>
```

### Request Headers

| Header   | Required | Value       | Description                                      |
|----------|----------|-------------|--------------------------------------------------|
| `Accept` | Yes      | `text/csv`  | Instructs the handler to format output as CSV    |

### Query Parameters

All standard filtering and pagination query parameters supported by `GET /api/v1/statements` apply to streamed CSV requests:

| Parameter         | Required | Description                                                  |
|-------------------|----------|--------------------------------------------------------------|
| `customer_id`     | Yes      | The customer ID whose statements are being exported          |
| `subscription_id` | No       | Filter by specific subscription UUID                         |
| `kind`            | No       | Filter by statement type (e.g. `invoice`, `credit_note`)     |
| `status`          | No       | Filter by lifecycle status (e.g. `open`, `paid`, `void`)     |
| `start_after`     | No       | RFC3339 lower bound timestamp (exclusive)                    |
| `end_before`      | No       | RFC3339 upper bound timestamp (exclusive)                    |
| `limit`           | No       | Page size limit (default 20, max 200)                        |
| `order`           | No       | Sort order by issuance date: `asc` or `desc` (default `desc`)|

---

## Response Schema

### Success Response (`200 OK`)

**Response Headers:**
```http
HTTP/1.1 200 OK
Content-Type: text/csv; charset=utf-8
Content-Disposition: attachment; filename="statements.csv"
```

**CSV Structure:**
The exported CSV file is UTF-8 encoded and formatted with standard comma delimiters. The first row contains schema column names matching the system's export standard:

```csv
id,subscription_id,customer_id,period_start,period_end,issued_at,total_amount,currency,kind,status
stmt-1,sub-1,cust-xyz,2025-01-01T00:00:00Z,2025-02-01T00:00:00Z,2025-02-02T00:00:00Z,2999,USD,invoice,paid
```

### Error Responses

If an error occurs during authentication, authorization, or query validation, the handler responds with standard JSON error envelopes (matching standard API error behavior):

| HTTP Status | Condition                                                      | Content-Type       |
|-------------|----------------------------------------------------------------|--------------------|
| `400`       | Missing `customer_id` or malformed RFC3339 timestamp / limit    | `application/json` |
| `401`       | Missing, expired, or invalid JWT authentication                | `application/json` |
| `403`       | Caller lacks permission to view statements for this customer   | `application/json` |
| `500`       | Internal repository or database execution error                | `application/json` |

---

## Security: OWASP CSV Injection (Formula Injection) Mitigation

### Threat Vector
When exported CSV files are opened in spreadsheet applications (Microsoft Excel, LibreOffice Calc, Google Sheets), cells starting with specific formula trigger characters can be interpreted as mathematical formulas or dynamic command expressions. Attackers could exploit this by injecting malicious payload strings into fields such as statement IDs, amounts, or custom metadata (e.g., `=cmd|' /C calc'!A0` or `=HYPERLINK(...)`), leading to arbitrary code execution or data exfiltration on the operator's workstation.

### Mitigation Implementation
To guarantee security without altering stored database records, the CSV streaming handler implements automatic formula escaping per **OWASP CSV Injection Guidance** via the `escapeCSVField` function in `internal/handlers/statements.go`.

When formatting any field value for CSV emission:
1. **Whitespace Trimming Check**: The field is evaluated both raw and stripped of leading whitespace (preventing bypasses via leading spaces or tabs such as `"   =cmd"`).
2. **Trigger Character Detection**: If the first character of the string or trimmed string matches any OWASP formula trigger:
   - `=` (equals)
   - `+` (plus)
   - `-` (minus)
   - `@` (at symbol)
   - `\t` (horizontal tab, ASCII 0x09)
   - `\r` (carriage return, ASCII 0x0D)
3. **Apostrophe Prepending**: The handler prepends a single apostrophe (`'`) to the value (e.g. `=1+1` becomes `'=1+1` and `-100` becomes `'-100`).

When spreadsheet parsers encounter a cell beginning with `'`, they treat the entire cell contents as literal text rather than an executable formula, neutralizing the injection risk while preserving visual clarity for the operator.

---

## Performance & Streaming Architecture

### Zero-Buffer Streaming
Unlike traditional export jobs that build large `bytes.Buffer` structures or concatenate strings in memory, `streamStatementsCSV` writes directly to the underlying HTTP `ResponseWriter` (`gin.Context.Writer`) via Go's standard `encoding/csv` writer.

- **Immediate Transmission**: The handler sends HTTP response headers and the CSV header row immediately upon invocation, calling `w.Flush()` to initiate network transfer without waiting for row iteration to finish.
- **Low Memory Footprint**: Statement rows are serialized and flushed incrementally. This prevents memory spikes when processing large statement lists.
- **Allocation Optimization**: For safe string fields requiring no escaping, `escapeCSVField` returns original slice pointers without heap allocations.
