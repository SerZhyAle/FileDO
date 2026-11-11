Imports System.Globalization

' SP-0196: the sentence that says why a run ended "Not proven".
'
' The CLI says why in a closed token on the `error` finding that ends the run (CLI-EVENT-STREAM rule
' 17), with the numbers the sentence needs under the keys that token's row in the contract names.
' The window turns the token into a sentence of its own language - never the CLI's English message,
' which a localized window must not show (APP-BEHAVIOUR rule 6) - and a run with no token, or a token
' this build does not know, gets one plain sentence that points at the output. A bare exit code is
' never the only explanation.
'
' The channel-side reasons (`Runner.Judge`: no result, a mismatch, a refused carrier) already say why
' and are not touched: this speaks only when the run itself said nothing the window could name.
Friend Module RunExplanation

    ' The tokens of rule 17 this window has a sentence for, each beside the keys of its numbers.
    Friend Const DestinationName As String = "destination-name"
    Friend Const FillInsufficientSpace As String = "fill-insufficient-space"
    Friend Const TestInsufficientSpace As String = "test-insufficient-space"
    Friend Const ContainerInsufficientSpace As String = "container-insufficient-space"
    Friend Const VolumeFull As String = "volume-full"
    Friend Const FileInUse As String = "file-in-use"

    ' The sentence for a Not proven run with no token this window can read.
    Friend Const GenericKey As String = "shell_why_generic"

    ' Every key this file asks the tables for - what the self-test holds the five languages to.
    Friend ReadOnly TableKeys As String() = {
        GenericKey, "shell_why_fill_space", "shell_why_test_space", "shell_why_container_space", "shell_why_volume_full", "shell_why_file_in_use",
        "shell_size_kb", "shell_size_mb", "shell_size_gb"}

    Private Const OneKiB As Double = 1024.0
    Private Const OneMiB As Double = 1024.0 * 1024.0
    Private Const OneGiB As Double = 1024.0 * 1024.0 * 1024.0

    ' The one sentence a Not proven result carries, or "" for any other verdict and for a run whose
    ' own reason (res.Reason) already says why.
    Friend Function SentenceFor(res As Runner.RunResult) As String
        Return SentenceFor(res, Localization.ForShell(), ShellCulture())
    End Function

    ' The same with the language named, so the self-test can read every table without changing the
    ' window's own setting.
    Friend Function SentenceFor(res As Runner.RunResult, dict As Dictionary(Of String, String), culture As CultureInfo) As String
        If res Is Nothing OrElse res.Verdict <> "Not proven" OrElse Not String.IsNullOrEmpty(res.Reason) Then Return ""
        Dim sentence = TokenSentence(res.ErrorReason, res.ErrorDetails, dict, culture)
        If sentence <> "" Then Return sentence
        Return Words(dict, GenericKey)
    End Function

    ' The sentence of one token, or "" when the token is unknown or a number its row names cannot be
    ' read (never a made-up zero: "0 MB free" for a missing key would be a false sentence).
    Friend Function TokenSentence(token As String, details As Dictionary(Of String, Object),
                                  dict As Dictionary(Of String, String), culture As CultureInfo) As String
        Select Case token
            Case DestinationName
                Return Localization.Format(Words(dict, "shell_fdsec_dest_name"), FdsecResults.RefusedCharacters)
            Case FillInsufficientSpace
                Dim fileBytes, freeBytes, reserveBytes As Long
                If Not TryBytes(details, "fileBytes", fileBytes) OrElse Not TryBytes(details, "freeBytes", freeBytes) OrElse
                   Not TryBytes(details, "reserveBytes", reserveBytes) Then Return ""
                Return Localization.Format(Words(dict, "shell_why_fill_space"),
                                           SizeText(fileBytes, dict, culture), SizeText(freeBytes, dict, culture),
                                           SizeText(reserveBytes, dict, culture))
            Case TestInsufficientSpace
                Dim requiredBytes, availableBytes As Long
                If Not TryBytes(details, "requiredBytes", requiredBytes) OrElse
                   Not TryBytes(details, "availableBytes", availableBytes) Then Return ""
                Return Localization.Format(Words(dict, "shell_why_test_space"),
                                           SizeText(requiredBytes, dict, culture), SizeText(availableBytes, dict, culture))
            Case ContainerInsufficientSpace
                Dim freeBytes, neededBytes As Long
                If Not TryBytes(details, "freeBytes", freeBytes) OrElse Not TryBytes(details, "neededBytes", neededBytes) Then Return ""
                Return Localization.Format(Words(dict, "shell_why_container_space"),
                                           SizeText(freeBytes, dict, culture), SizeText(neededBytes, dict, culture))
            Case VolumeFull
                Return Words(dict, "shell_why_volume_full")
            Case FileInUse
                Return Words(dict, "shell_why_file_in_use")
        End Select
        Return ""
    End Function

    ' A size in the language of the window (D4): the culture's own decimal mark and the language's own
    ' unit names, binary steps as the CLI counts them. Up to two decimals, none when whole.
    Friend Function SizeText(bytes As Long, dict As Dictionary(Of String, String), culture As CultureInfo) As String
        Dim size = CDbl(Math.Max(bytes, 0L))
        Dim unitKey As String
        If size >= OneGiB Then
            size /= OneGiB
            unitKey = "shell_size_gb"
        ElseIf size >= OneMiB Then
            size /= OneMiB
            unitKey = "shell_size_mb"
        Else
            size /= OneKiB
            unitKey = "shell_size_kb"
        End If
        Return size.ToString("0.##", culture) & " " & Words(dict, unitKey)
    End Function

    ' A number the contract's row names, read as the integer it is. A missing key, a null, a string, a
    ' fraction or a negative amount is "cannot read", not zero.
    Friend Function TryBytes(details As Dictionary(Of String, Object), key As String, ByRef value As Long) As Boolean
        value = 0
        If details Is Nothing Then Return False
        Dim raw As Object = Nothing
        If Not details.TryGetValue(key, raw) OrElse raw Is Nothing Then Return False
        Try
            Select Case Type.GetTypeCode(raw.GetType())
                Case TypeCode.Byte, TypeCode.SByte, TypeCode.Int16, TypeCode.UInt16, TypeCode.Int32, TypeCode.UInt32, TypeCode.Int64
                    value = Convert.ToInt64(raw, CultureInfo.InvariantCulture)
                Case TypeCode.UInt64
                    value = Convert.ToInt64(raw, CultureInfo.InvariantCulture)
                Case TypeCode.Double, TypeCode.Decimal
                    Dim d = Convert.ToDouble(raw, CultureInfo.InvariantCulture)
                    If Double.IsNaN(d) OrElse Double.IsInfinity(d) OrElse d <> Math.Floor(d) OrElse Math.Abs(d) > 9.0E+18 Then Return False
                    value = Convert.ToInt64(d)
                Case Else
                    Return False
            End Select
        Catch ex As OverflowException
            value = 0
            Return False
        End Try
        If value < 0 Then
            value = 0
            Return False
        End If
        Return True
    End Function

    ' A table's words for a key; the key itself when no table has it, as Localization.T does.
    Private Function Words(dict As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return Localization.Multiline(v)
        Return key
    End Function

    ' The culture of the window's language, so a decimal mark follows the words around it.
    Private Function ShellCulture() As CultureInfo
        Try
            Return CultureInfo.GetCultureInfo(ShellSettings.Language())
        Catch ex As CultureNotFoundException
            Return CultureInfo.InvariantCulture
        End Try
    End Function

End Module
