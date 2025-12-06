# PII Filtering Example

This example demonstrates protecting sensitive data using AgentPG's hook system.

## Features

- **Pattern-Based Detection**: Regex patterns for common PII types
- **Multiple Modes**: Block, redact, or warn
- **Audit Logging**: Track all blocked messages
- **Customizable Patterns**: Add domain-specific patterns

## PII Patterns

The example detects:

| Type | Pattern | Example |
|------|---------|---------|
| SSN | `\d{3}-\d{2}-\d{4}` | 123-45-6789 |
| Credit Card | `\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}` | 4532-1234-5678-9012 |
| Email | `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z]{2,}` | user@example.com |
| Phone | `\(?\d{3}\)?[- ]?\d{3}[- ]?\d{4}` | (555) 123-4567 |
| API Key | `sk-[a-zA-Z0-9]{20,}` | sk-abc123... |
| AWS Key | `AKIA[0-9A-Z]{16}` | AKIAIOSFODNN7EXAMPLE |

## Filter Modes

### 1. Block Mode (Default)
Completely blocks messages containing PII:
```go
piiFilter := NewPIIFilter(ModeBlock)
// Returns error, message not sent to Claude
```

### 2. Redact Mode
Replaces PII with placeholders:
```go
piiFilter := NewPIIFilter(ModeRedact)
// "Email: john@example.com" → "Email: [REDACTED:Email]"
```

### 3. Warn Mode
Logs warning but allows message:
```go
piiFilter := NewPIIFilter(ModeWarn)
// Logs detection, message still sent
```

## Integration

Use the OnBeforeMessage hook:

```go
agent.OnBeforeMessage(func(ctx context.Context, sessionID string, prompt string) error {
    detected, _ := piiFilter.Check(prompt)

    if len(detected) > 0 {
        piiFilter.Record(sessionID, detected, prompt)
        return &ErrPIIDetected{Types: detected}
    }

    return nil
})
```

## Sample Output

```
Test 1: Normal message
  Message: What's the weather like today?
  Result: ALLOWED - Response: The weather is...

Test 2: Contains SSN
  Message: My SSN is 123-45-6789, can you remember it?
  [PII BLOCKED] Detected: SSN
  Result: BLOCKED (as expected: true)

Test 3: Contains email
  Message: Send the report to john.doe@company.com
  [PII BLOCKED] Detected: Email
  Result: BLOCKED (as expected: true)
```

## Audit Log

All blocked messages are logged:

```
=== Audit Log ===
1. [10:30:05] Session: abc12345...
   Type: SSN
   Preview: My SSN is 123-45-6789, can you...

2. [10:30:06] Session: abc12345...
   Type: Email
   Preview: Send the report to john.doe@comp...
```

## Redaction Demo

```
Original: Contact john@example.com or call (555) 123-4567. SSN: 123-45-6789
Detected: Email, Phone, SSN
Redacted: Contact [REDACTED:Email] or call [REDACTED:Phone]. SSN: [REDACTED:SSN]
```

## Custom Patterns

Add domain-specific patterns:

```go
// Add custom pattern
filter.patterns["EmployeeID"] = regexp.MustCompile(`\bEMP-\d{6}\b`)

// Add medical record number
filter.patterns["MRN"] = regexp.MustCompile(`\bMRN-\d{8}\b`)
```

## Production Considerations

1. **False Positives**: Test patterns thoroughly
2. **Performance**: Compile regex once, reuse
3. **Compliance**: Document for GDPR, HIPAA, etc.
4. **Monitoring**: Alert on high block rates
5. **User Feedback**: Tell users why message was blocked

## Legal Compliance

PII filtering helps with:
- **GDPR**: Prevent accidental data sharing
- **HIPAA**: Protect health information
- **PCI-DSS**: Block credit card data
- **SOC 2**: Demonstrate data protection controls

## Running

```bash
export ANTHROPIC_API_KEY="your-api-key"
export DATABASE_URL="postgres://user:pass@localhost:5432/agentpg"

go run main.go
```
