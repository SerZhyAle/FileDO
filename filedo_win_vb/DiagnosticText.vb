Imports System.IO
Imports System.Text
Imports System.Text.RegularExpressions

' DIAGNOSTIC-REPORT rule 3: export is a privacy boundary, including exception text.
' Credential-bearing lines are removed in full so quoted, malformed and multiline values
' cannot escape a token-only replacement. Source logs on disk remain available locally.
Friend Module DiagnosticText
    Private ReadOnly risk As New Regex(
        "(?i)(?:\b[\w-]*(?:password|passwd|passphrase|token|auth|credential|secret|salt)[\w-]*\b|\b(?:pass|pwd|pin|api[_-]?key|private[_ -]?key|x-amz-[\w-]+|wmsAuthSign|hdnts|hdnea|policy|key-pair-id)\b|(?:^|[\s""'])[pP]:|\b[a-z][a-z0-9+.-]*://|-----BEGIN)",
        RegexOptions.CultureInvariant, TimeSpan.FromMilliseconds(100))
    Private ReadOnly paths As New Regex(
        "(?i)(?:[a-z]:[\\/]|\\\\|/data/user/|/home/|/Users/)",
        RegexOptions.CultureInvariant, TimeSpan.FromMilliseconds(100))

    Friend Function Sanitize(text As String) As String
        Dim result As New StringBuilder()
        Dim keyBlock As Boolean = False
        Dim quotedCredential As Boolean = False
        Dim compactedFragment As Boolean = False
        Using reader As New StringReader(text)
            While True
                Dim line = reader.ReadLine()
                If line Is Nothing Then Exit While
                ' A raw compacted fragment can start inside a key or quoted credential.
                ' Drop opaque continuation lines until the next timestamped log record.
                If line.StartsWith("[Diag] LOG COMPACTED", StringComparison.Ordinal) Then
                    result.AppendLine(SanitizeLine(line))
                    compactedFragment = True
                    Continue While
                End If
                If compactedFragment Then
                    If Not Regex.IsMatch(line, "^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} (?:INFO|ERROR|DEBUG) ") Then
                        result.AppendLine("[REDACTED]")
                        Continue While
                    End If
                    compactedFragment = False
                    quotedCredential = False
                    keyBlock = False
                End If
                If quotedCredential Then
                    If Regex.IsMatch(line, "^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} (?:INFO|ERROR|DEBUG) ") Then
                        quotedCredential = False
                        keyBlock = False
                    Else
                        result.AppendLine("[REDACTED]")
                        If line.Contains("""") Then quotedCredential = False
                        Continue While
                    End If
                End If
                If line.IndexOf("-----BEGIN", StringComparison.OrdinalIgnoreCase) >= 0 Then keyBlock = True
                If keyBlock Then
                    result.AppendLine("[REDACTED]")
                    If line.IndexOf("-----END", StringComparison.OrdinalIgnoreCase) >= 0 Then keyBlock = False
                    Continue While
                End If
                Dim clean = SanitizeLine(line)
                If clean = "[REDACTED]" AndAlso line.Count(Function(c) c = """"c) Mod 2 = 1 Then quotedCredential = True
                result.AppendLine(clean)
            End While
        End Using
        Return result.ToString()
    End Function

    Friend Function SanitizeLine(line As String) As String
        ' Bound work even for one deliberately malformed line; never pass it through on failure.
        If line.Length > 65536 Then Return "[Diag] LINE OMITTED | dropped_line_bytes=" & Encoding.UTF8.GetByteCount(line).ToString()
        Try
            If risk.IsMatch(line) OrElse paths.IsMatch(line) Then Return "[REDACTED]"
            Return line
        Catch ex As RegexMatchTimeoutException
            Return "[Diag] PATH REDACTION TIMEOUT | dropped_line_bytes=" & Encoding.UTF8.GetByteCount(line).ToString()
        End Try
    End Function
End Module
