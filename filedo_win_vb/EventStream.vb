Imports System.IO
Imports System.Web.Script.Serialization

' Structured event channel tailer and parser - the consumer side of the
' CLI-EVENT-STREAM contract, which lives outside this repository (AGENTS.md
' "External contracts", docs\contracts\CLI-EVENT-STREAM.md). Every member and
' field is read by name and an absent one takes the contract's documented
' default (rules 6 and 7); an unknown kind is raised and acted on no further
' (rule 8).
Public Class EventStream
    Implements IDisposable

    Public Class ShellEvent
        Public Property SchemaVersion As Integer
        Public Property Kind As String
        Public Property Timestamp As DateTime
        Public Property Data As Dictionary(Of String, Object)
    End Class

    Public Class ProgressInfo
        Public Property DoneItems As Long
        Public Property TotalItems As Long
        Public Property DoneBytes As Long
        Public Property TotalBytes As Long
        Public Property SpeedBps As Double
        Public Property Message As String
    End Class

    Public Class ResultInfo
        Public Property Verdict As String
        Public Property Numbers As Dictionary(Of String, Object)
        Public Property FilesLeft As List(Of String)
        Public Property Reports As List(Of String)
    End Class

    ' The MAJOR this window knows how to read. It is the wire carrier of the
    ' contract (rule 6) and the one thing a reader is obliged to dispatch on.
    Public Const KnownSchemaVersion As Integer = 1

    Public Event EventReceived(ev As ShellEvent)
    Public Event StepChanged(stepName As String, description As String)
    Public Event ProgressReported(progress As ProgressInfo)
    Public Event FindingReported(findingType As String, message As String, details As Dictionary(Of String, Object))
    Public Event NoteReported(message As String)
    Public Event ResultReceived(result As ResultInfo)

    ' Raised once, when a line declares a MAJOR above KnownSchemaVersion.
    ' Interpretation stops there and does not resume: the lines this window
    ' does understand are exactly the ones that would not have changed the
    ' verdict (contract section 3).
    Public Event UnsupportedVersion(found As Integer)

    Private ReadOnly filePath As String
    Private ReadOnly jsonSerializer As New JavaScriptSerializer()
    Private lastOffset As Long = 0
    Private disposed As Boolean = False
    Private refusedVersion As Boolean = False

    Public Sub New(filePath As String)
        Me.filePath = filePath
    End Sub

    ' True once the channel has been refused for declaring a newer MAJOR. The
    ' run is then *Not proven* whatever else the file contains.
    Public ReadOnly Property VersionRefused As Boolean
        Get
            Return refusedVersion
        End Get
    End Property

    ' Tail by byte offset, and never advance past a line whose terminator has
    ' not been written yet (rule 4).
    '
    ' The previous shape read to EndOfStream and then took fs.Position, so a
    ' final line still being appended was consumed, failed to parse, and was
    ' swallowed - one event lost, at random, under load, and the event most
    ' likely to be mid-write is the last one, which is the `result`. Here the
    ' offset moves only as far as the last newline actually seen, so a partial
    ' line is simply read again on the next poll.
    Public Sub Poll()
        If disposed OrElse refusedVersion OrElse Not File.Exists(filePath) Then Return

        Dim buffer As Byte()
        Try
            Using fs As New FileStream(filePath, FileMode.Open, FileAccess.Read, FileShare.ReadWrite)
                If fs.Length <= lastOffset Then Return

                Dim pending As Long = fs.Length - lastOffset
                buffer = New Byte(CInt(pending) - 1) {}
                fs.Seek(lastOffset, SeekOrigin.Begin)

                Dim got As Integer = 0
                While got < buffer.Length
                    Dim n = fs.Read(buffer, got, buffer.Length - got)
                    If n <= 0 Then Exit While
                    got += n
                End While
                If got < buffer.Length Then
                    ReDim Preserve buffer(got - 1)
                End If
            End Using
        Catch
            ' File contention during a concurrent append: nothing is consumed,
            ' so the next poll reads the same bytes.
            Return
        End Try

        If buffer Is Nothing OrElse buffer.Length = 0 Then Return

        ' Only whole lines are interpreted, and the offset advances only over
        ' them. A trailing fragment stays in front of the offset.
        Dim lastNewline As Integer = Array.LastIndexOf(buffer, CByte(10))
        If lastNewline < 0 Then Return

        Dim complete = System.Text.Encoding.UTF8.GetString(buffer, 0, lastNewline + 1)
        lastOffset += (lastNewline + 1)

        For Each line In complete.Split(New Char() {ChrW(10)})
            If refusedVersion Then Exit For
            Dim trimmed = line.TrimEnd(ChrW(13))
            If Not String.IsNullOrWhiteSpace(trimmed) Then
                ParseAndDispatchLine(trimmed)
            End If
        Next
    End Sub

    Private Sub ParseAndDispatchLine(line As String)
        Try
            Dim dict = jsonSerializer.Deserialize(Of Dictionary(Of String, Object))(line)
            If dict Is Nothing Then Return

            ' A line that omits schemaVersion is read as 1 for the life of
            ' MAJOR 1 (contract section 3); a higher MAJOR stops interpretation
            ' altogether rather than being half-read.
            Dim version As Integer
            If Not TryGetSchemaVersion(dict, version) Then
                ' A present carrier that is not a JSON integer is no safer to
                ' interpret than a newer MAJOR.  Refuse it permanently rather
                ' than silently treating it as the known version (§3).
                refusedVersion = True
                RaiseEvent UnsupportedVersion(KnownSchemaVersion + 1)
                Return
            End If
            If version > KnownSchemaVersion Then
                refusedVersion = True
                RaiseEvent UnsupportedVersion(version)
                Return
            End If

            ' Every field below degrades to the documented default of rule 7
            ' rather than costing the event that carried it. A timestamp
            ' nobody reads used to be able to drop a whole `result` when a
            ' culture would not parse it: the field is optional, the event is
            ' not.
            Dim ev As New ShellEvent With {
                .SchemaVersion = version,
                .Kind = GetString(dict, "kind"),
                .Timestamp = GetDate(dict, "timestamp"),
                .Data = If(dict.ContainsKey("data") AndAlso TypeOf dict("data") Is Dictionary(Of String, Object),
                           DirectCast(dict("data"), Dictionary(Of String, Object)), New Dictionary(Of String, Object)())
            }

            RaiseEvent EventReceived(ev)

            ' Contract tokens are ordinal and case-sensitive. "RESULT" is an
            ' unknown kind, not a result that happened to be capitalized.
            Select Case ev.Kind
                Case "step"
                    Dim name = If(ev.Data.ContainsKey("name"), ev.Data("name").ToString(), "")
                    Dim desc = If(ev.Data.ContainsKey("description"), ev.Data("description").ToString(), "")
                    RaiseEvent StepChanged(name, desc)

                Case "progress"
                    Dim p As New ProgressInfo With {
                        .DoneItems = GetLong(ev.Data, "doneItems"),
                        .TotalItems = GetLong(ev.Data, "totalItems"),
                        .DoneBytes = GetLong(ev.Data, "doneBytes"),
                        .TotalBytes = GetLong(ev.Data, "totalBytes"),
                        .SpeedBps = GetDouble(ev.Data, "speedBps"),
                        .Message = GetString(ev.Data, "message")
                    }
                    RaiseEvent ProgressReported(p)

                Case "finding"
                    Dim fType = GetString(ev.Data, "type")
                    Dim msg = GetString(ev.Data, "message")
                    Dim details As Dictionary(Of String, Object) = Nothing
                    If ev.Data.ContainsKey("details") AndAlso TypeOf ev.Data("details") Is Dictionary(Of String, Object) Then
                        details = DirectCast(ev.Data("details"), Dictionary(Of String, Object))
                    End If
                    RaiseEvent FindingReported(fType, msg, details)

                Case "note"
                    Dim msg = GetString(ev.Data, "message")
                    RaiseEvent NoteReported(msg)

                Case "result"
                    Dim r As New ResultInfo With {
                        .Verdict = GetString(ev.Data, "verdict"),
                        .Numbers = If(ev.Data.ContainsKey("numbers") AndAlso TypeOf ev.Data("numbers") Is Dictionary(Of String, Object),
                                      DirectCast(ev.Data("numbers"), Dictionary(Of String, Object)), New Dictionary(Of String, Object)()),
                        .FilesLeft = GetStringList(ev.Data, "filesLeft"),
                        .Reports = GetStringList(ev.Data, "reports")
                    }
                    RaiseEvent ResultReceived(r)

                    ' Any other kind is recorded and not acted on (rule 8): it
                    ' may never be mapped onto a kind it resembles, and it may
                    ' never abort the run.
            End Select
        Catch
            ' Only a line that is not a JSON object at all reaches here now.
            ' Every field inside one degrades to its documented default above,
            ' so a bad field can no longer cost the event that carried it.
        End Try
    End Sub

    ' schemaVersion is the load-bearing JSON int. Its absence means MAJOR 1;
    ' its presence as any other JSON type is refused (contract §3).
    Private Shared Function TryGetSchemaVersion(dict As Dictionary(Of String, Object), ByRef version As Integer) As Boolean
        version = KnownSchemaVersion
        If dict Is Nothing OrElse Not dict.ContainsKey("schemaVersion") Then Return True
        Dim value = dict("schemaVersion")
        If value Is Nothing Then Return False
        Select Case Type.GetTypeCode(value.GetType())
            Case TypeCode.Byte, TypeCode.SByte, TypeCode.Int16, TypeCode.UInt16, TypeCode.Int32
                version = Convert.ToInt32(value)
                Return True
            Case TypeCode.Int64, TypeCode.UInt32, TypeCode.UInt64
                Try
                    version = Convert.ToInt32(value)
                    Return True
                Catch
                    Return False
                End Try
        End Select
        Return False
    End Function

    ' The timestamp is informational: RFC 3339 with an offset (rule 6), read
    ' with the invariant culture so a machine in any locale reads the same
    ' bytes the producer wrote. An unparseable one becomes "now" and the event
    ' survives - which is the whole point, because the event it used to cost
    ' was whichever one carried the verdict.
    Private Shared Function GetDate(dict As Dictionary(Of String, Object), key As String) As DateTime
        If dict IsNot Nothing AndAlso dict.ContainsKey(key) AndAlso dict(key) IsNot Nothing Then
            Dim parsed As DateTime
            If DateTime.TryParse(dict(key).ToString(),
                                 Globalization.CultureInfo.InvariantCulture,
                                 Globalization.DateTimeStyles.RoundtripKind, parsed) Then
                Return parsed
            End If
        End If
        Return DateTime.Now
    End Function

    Private Shared Function GetString(dict As Dictionary(Of String, Object), key As String) As String
        If dict IsNot Nothing AndAlso dict.ContainsKey(key) AndAlso dict(key) IsNot Nothing Then
            Return dict(key).ToString()
        End If
        Return ""
    End Function

    Private Shared Function GetLong(dict As Dictionary(Of String, Object), key As String) As Long
        If dict IsNot Nothing AndAlso dict.ContainsKey(key) AndAlso dict(key) IsNot Nothing Then
            Try
                Return Convert.ToInt64(dict(key))
            Catch
            End Try
        End If
        Return 0L
    End Function

    Private Shared Function GetDouble(dict As Dictionary(Of String, Object), key As String) As Double
        If dict IsNot Nothing AndAlso dict.ContainsKey(key) AndAlso dict(key) IsNot Nothing Then
            Try
                Return Convert.ToDouble(dict(key))
            Catch
            End Try
        End If
        Return 0.0
    End Function

    Private Shared Function GetStringList(dict As Dictionary(Of String, Object), key As String) As List(Of String)
        Dim res As New List(Of String)()
        If dict IsNot Nothing AndAlso dict.ContainsKey(key) AndAlso dict(key) IsNot Nothing Then
            Dim arr = TryCast(dict(key), ArrayList)
            If arr IsNot Nothing Then
                For Each item In arr
                    If item IsNot Nothing Then res.Add(item.ToString())
                Next
            End If
        End If
        Return res
    End Function

    Public Sub Dispose() Implements IDisposable.Dispose
        disposed = True
    End Sub

End Class
