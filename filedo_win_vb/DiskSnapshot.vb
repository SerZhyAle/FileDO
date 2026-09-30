Imports System.Globalization
Imports System.IO
Imports System.Text
Imports System.Web.Script.Serialization

' What the window knows about every disk at once (SP-0063 section 8.1): the snapshot
' `filedo.exe vd status json` prints, read into plain values.
'
' The window learns state from filedo.exe and from nothing else - it never opens vdisk-state.json,
' vd-registry.json or a container (SP-0004 6.4, "the console answers, the window does not grow a
' second reader"). The snapshot is an in-repo wire shape (SP-0063 D2), defined where it is written,
' cmd\filedo\vdisk_status_json.go. Its reader here keeps to the compatibility law all the same: an
' unknown field is ignored, a missing optional one takes its default, and a schema or a version it
' does not know is refused as a whole rather than half read.

' One disk of the snapshot: a container (registered, or mounted by path and not registered) or a
' foreign image FileDO mounted.
Public Class DiskRecord
    Public Property Kind As String = "container"
    Public Property Name As String = ""
    Public Property Path As String = ""
    Public Property ContainerId As String = ""
    Public Property Registered As Boolean = False
    ' ok, missing, different or unreadable (with FileError).
    Public Property FileState As String = "ok"
    Public Property FileError As String = ""
    Public Property Profile As String = ""
    Public Property Protection As DiskProtection = DiskProtection.Unknown
    Public Property LogicalSize As Long = 0
    ' Nothing when the header was not read.
    Public Property Clean As Boolean? = Nothing
    Public Property LastGoodSave As DateTimeOffset? = Nothing
    Public Property AutoMount As Boolean = False
    ' "X:" while mounted, "" otherwise.
    Public Property Letter As String = ""
    Public Property [ReadOnly] As Boolean = False
    Public Property MountedAt As DateTimeOffset? = Nothing
    Public Property ServerAlive As Boolean = False
    Public Property HasRam As Boolean = False
    Public Property RamDirty As Long = 0
    Public Property RamSaving As Boolean = False
    Public Property RamLastGoodSave As DateTimeOffset? = Nothing

    Public ReadOnly Property IsImage As Boolean
        Get
            Return Kind = "image"
        End Get
    End Property

    Public ReadOnly Property IsMounted As Boolean
        Get
            Return Letter <> ""
        End Get
    End Property

    Public ReadOnly Property IsRam As Boolean
        Get
            Return String.Equals(Profile, "ram", StringComparison.OrdinalIgnoreCase)
        End Get
    End Property

    ' Who a row is from one snapshot to the next: the container id (SP-0063 5.1 - joined by id,
    ' never by path), or an image's path. A row the snapshot carries no id for keeps its path.
    Public ReadOnly Property Key As String
        Get
            If IsImage Then Return "image:" & Path.ToLowerInvariant()
            If ContainerId <> "" Then Return "id:" & ContainerId.ToLowerInvariant()
            Return "path:" & Path.ToLowerInvariant()
        End Get
    End Property

    ' What the row is called before the window translates anything: the registry name, or the file
    ' name of a container mounted by path. An image's name is the window's "(image)".
    Public ReadOnly Property BaseName As String
        Get
            If Name <> "" Then Return Name
            ' By hand rather than Path.GetFileNameWithoutExtension, which throws on a character .NET
            ' Framework calls invalid - and a name on screen is no reason for a window to fail.
            Dim p = If(Path, "")
            Dim slash = Math.Max(p.LastIndexOf("\"c), p.LastIndexOf("/"c))
            Dim file = p.Substring(slash + 1)
            Dim dot = file.LastIndexOf("."c)
            Return If(dot > 0, file.Substring(0, dot), file)
        End Get
    End Property

    ' The image's kind for the Profile column: vhd, vhdx or iso, from its extension.
    Public ReadOnly Property ImageFormat As String
        Get
            Dim dot = Path.LastIndexOf("."c)
            If dot < 0 OrElse dot < Path.LastIndexOf("\"c) Then Return ""
            Return Path.Substring(dot + 1).ToLowerInvariant()
        End Get
    End Property
End Class

' The shutdown guard's state (SP-0080 4-5), as the snapshot's `guard` object said it - Nothing
' when the CLI that answered predates the field, which reads as "off" and nothing more. Its rows
' are the last run's: what the guard did to each container at the session's end.
Public Class DiskGuardRunRow
    Public Property Name As String = ""
    Public Property Path As String = ""
    ' save or unmount.
    Public Property Action As String = ""
    ' saved, unmounted, skipped or unfinished.
    Public Property Outcome As String = ""
    Public Property Reason As String = ""
    Public Property BytesSaved As Long = 0
End Class

Public Class DiskGuardState
    Public Property Installed As Boolean = False
    Public Property Running As Boolean = False
    Public Property LastRun As DateTimeOffset? = Nothing
    ' The session's end is one thing (SP-0080 D7): the value is "session" or empty.
    Public Property Ended As String = ""
    Public ReadOnly Property Containers As New List(Of DiskGuardRunRow)

    ' Whether the last run left something behind: a row the report cannot call saved or unmounted.
    Public ReadOnly Property HasLeftovers As Boolean
        Get
            Return Containers.Any(Function(r) r.Outcome <> "saved" AndAlso r.Outcome <> "unmounted")
        End Get
    End Property
End Class

Public Class DiskSnapshot
    Public Const Schema As String = "filedo.vd-status"
    ' The MAJOR this build reads. A higher one is refused with the sentence that says to update.
    Public Const KnownVersion As Integer = 1

    Public Property Version As Integer = 0
    Public Property At As DateTimeOffset? = Nothing
    Public Property Packaged As Boolean = False
    Public Property TransportReady As Boolean = False
    Public Property InitiatorService As String = ""
    ' "", packaged, initiator_missing, initiator_disabled or service_manager.
    Public Property TransportReason As String = ""
    ' The shutdown guard (SP-0080): Nothing when the snapshot carries no guard field, which an
    ' older CLI's document does not - the unknown-field rule, read from this side.
    Public Property Guard As DiskGuardState = Nothing
    Public ReadOnly Property Disks As New List(Of DiskRecord)

    Public ReadOnly Property MountedCount As Integer
        Get
            Return Disks.Where(Function(d) d.IsMounted).Count()
        End Get
    End Property

    ' Reads the snapshot out of what filedo.exe printed. The document is one line that starts with
    ' {"schema": - found as that line, so a warning on the same stream never breaks the read.
    ' Returns Nothing with problem set to a localization key when the text holds no snapshot this
    ' build can read.
    Public Shared Function Parse(output As String, ByRef problem As String) As DiskSnapshot
        problem = ""
        Dim line As String = Nothing
        For Each raw In If(output, "").Split(New String() {vbCrLf, vbLf}, StringSplitOptions.RemoveEmptyEntries)
            Dim t = raw.Trim()
            If t.StartsWith("{", StringComparison.Ordinal) AndAlso t.Contains("""schema""") Then
                line = t
                Exit For
            End If
        Next
        If line Is Nothing Then
            problem = "vd_mgr_stale_failed"
            Return Nothing
        End If

        Dim doc As Dictionary(Of String, Object)
        Try
            Dim js As New JavaScriptSerializer() With {.MaxJsonLength = Integer.MaxValue}
            doc = js.Deserialize(Of Dictionary(Of String, Object))(line)
        Catch ex As Exception
            ShellLog.Write("read the disk snapshot", ex)
            problem = "vd_mgr_stale_format"
            Return Nothing
        End Try
        If doc Is Nothing OrElse Str(doc, "schema") <> Schema Then
            problem = "vd_mgr_stale_format"
            Return Nothing
        End If
        Dim s As New DiskSnapshot With {.Version = CInt(Num(doc, "version"))}
        If s.Version < 1 OrElse s.Version > KnownVersion Then
            problem = "vd_mgr_stale_format"
            Return Nothing
        End If
        s.At = Stamp(doc, "at")
        s.Packaged = Bool(doc, "packaged")
        Dim tr = Obj(doc, "transport")
        If tr IsNot Nothing Then
            s.TransportReady = Bool(tr, "ready")
            s.InitiatorService = Str(tr, "initiator_service")
            s.TransportReason = Str(tr, "reason")
        End If
        s.Guard = GuardOf(doc)
        Dim list = TryCast(Value(doc, "disks"), IEnumerable)
        If list Is Nothing Then
            ' A snapshot with no list is not "nothing is mounted" - it is a snapshot that says nothing.
            problem = "vd_mgr_stale_format"
            Return Nothing
        End If
        For Each item In list
            Dim d = TryCast(item, Dictionary(Of String, Object))
            If d Is Nothing Then Continue For
            s.Disks.Add(RecordOf(d))
        Next
        Return s
    End Function

    ' The `guard` object, or Nothing when the document has none. A field this build does not know
    ' inside it is ignored, exactly as a field it does not know at the top is.
    Private Shared Function GuardOf(doc As Dictionary(Of String, Object)) As DiskGuardState
        Dim g = Obj(doc, "guard")
        If g Is Nothing Then Return Nothing
        Dim out As New DiskGuardState With {
            .Installed = Bool(g, "installed"),
            .Running = Bool(g, "running"),
            .LastRun = Stamp(g, "last_run"),
            .Ended = Str(g, "ended")}
        Dim rows = TryCast(Value(g, "containers"), IEnumerable)
        If rows IsNot Nothing Then
            For Each item In rows
                Dim r = TryCast(item, Dictionary(Of String, Object))
                If r Is Nothing Then Continue For
                out.Containers.Add(New DiskGuardRunRow With {
                    .Name = Str(r, "name"),
                    .Path = Str(r, "path"),
                    .Action = Str(r, "action"),
                    .Outcome = Str(r, "outcome"),
                    .Reason = Str(r, "reason"),
                    .BytesSaved = CLng(Num(r, "bytes_saved"))})
            Next
        End If
        Return out
    End Function

    Private Shared Function RecordOf(d As Dictionary(Of String, Object)) As DiskRecord
        Dim r As New DiskRecord With {
            .Kind = If(Str(d, "kind") = "image", "image", "container"),
            .Name = Str(d, "name"),
            .Path = Str(d, "path"),
            .ContainerId = Str(d, "container_id"),
            .Registered = Bool(d, "registered"),
            .FileState = If(Str(d, "file") = "", "ok", Str(d, "file")),
            .FileError = Str(d, "file_error"),
            .Profile = Str(d, "profile"),
            .LogicalSize = CLng(Num(d, "logical_size")),
            .LastGoodSave = Stamp(d, "last_good_save"),
            .AutoMount = Bool(d, "auto")
        }
        ' The two words and nothing in between (SP-0063 principle 3): anything else is not known.
        Select Case Str(d, "protection")
            Case "obfuscated" : r.Protection = DiskProtection.Obfuscated
            Case "encrypted" : r.Protection = DiskProtection.Encrypted
        End Select
        Dim c = Value(d, "clean")
        If TypeOf c Is Boolean Then r.Clean = CBool(c)

        If r.IsImage Then
            r.Letter = LetterOf(Str(d, "letter"))
            r.MountedAt = Stamp(d, "mounted_at")
            r.ServerAlive = True
            Return r
        End If
        Dim m = Obj(d, "mount")
        If m IsNot Nothing Then
            r.Letter = LetterOf(Str(m, "letter"))
            r.ReadOnly = Bool(m, "read_only")
            r.MountedAt = Stamp(m, "mounted_at")
            r.ServerAlive = Bool(m, "server_alive")
            Dim ram = Obj(m, "ram")
            If ram IsNot Nothing Then
                r.HasRam = True
                r.RamDirty = CLng(Num(ram, "dirty_bytes"))
                r.RamSaving = Bool(ram, "saving")
                r.RamLastGoodSave = Stamp(ram, "last_good_save")
            End If
        End If
        Return r
    End Function

    ' ---- the JSON values, each with its default ---------------------------------

    Private Shared Function Value(d As Dictionary(Of String, Object), key As String) As Object
        Dim v As Object = Nothing
        If d IsNot Nothing AndAlso d.TryGetValue(key, v) Then Return v
        Return Nothing
    End Function

    Private Shared Function Obj(d As Dictionary(Of String, Object), key As String) As Dictionary(Of String, Object)
        Return TryCast(Value(d, key), Dictionary(Of String, Object))
    End Function

    Private Shared Function Str(d As Dictionary(Of String, Object), key As String) As String
        Dim v = TryCast(Value(d, key), String)
        Return If(v, "")
    End Function

    Private Shared Function Bool(d As Dictionary(Of String, Object), key As String) As Boolean
        Dim v = Value(d, key)
        Return TypeOf v Is Boolean AndAlso CBool(v)
    End Function

    Private Shared Function Num(d As Dictionary(Of String, Object), key As String) As Decimal
        Dim v = Value(d, key)
        If v Is Nothing Then Return 0D
        If Not (TypeOf v Is Integer OrElse TypeOf v Is Long OrElse TypeOf v Is Decimal OrElse TypeOf v Is Double) Then Return 0D
        Try
            Return Convert.ToDecimal(v, CultureInfo.InvariantCulture)
        Catch
            Return 0D
        End Try
    End Function

    Private Shared Function Stamp(d As Dictionary(Of String, Object), key As String) As DateTimeOffset?
        Dim s = Str(d, key)
        If s = "" Then Return Nothing
        Dim t As DateTimeOffset
        If DateTimeOffset.TryParse(s, CultureInfo.InvariantCulture, DateTimeStyles.None, t) Then Return t
        Return Nothing
    End Function

    Private Shared Function LetterOf(s As String) As String
        Dim t = If(s, "").Trim().TrimEnd("\"c)
        Return If(TargetPath.IsDriveToken(t), t.ToUpperInvariant(), "")
    End Function

End Class

' Asks filedo.exe for the snapshot, off the window's thread: `filedo.exe --no-history vd status
' json`, which reads headers only and needs no credential. The child is started the way every run
' is (Runner.NewStartInfo): no window, stdin closed, in the window's job - and it is killed when it
' has not answered in eight seconds (SP-0063 8.2), the DiskProbe bound.
Public Module DiskStateProbe

    Friend Const TimeoutMs As Integer = 8000

    ' The command line, for the self-test and for the report of a failed read.
    Friend ReadOnly Arguments As String() = {"--no-history", "vd", "status", "json"}

    ' Returns the snapshot, or Nothing with problem set to the key of what went wrong.
    Public Function Read(ByRef problem As String) As DiskSnapshot
        problem = ""
        If Not DiskProbe.Enabled Then
            problem = "vd_mgr_stale_failed"
            Return Nothing
        End If
        Try
            Dim psi = Runner.NewStartInfo(Runner.LocateCLI(), ArgQuoting.JoinArgs(Arguments), Runner.GetAppDataDir(), False)
            Dim sb As New StringBuilder()
            Dim gate As New Object()
            Using p As New Process With {.StartInfo = psi}
                AddHandler p.OutputDataReceived, Sub(s, e)
                                                     If e.Data Is Nothing Then Return
                                                     SyncLock gate
                                                         sb.AppendLine(e.Data)
                                                     End SyncLock
                                                 End Sub
                AddHandler p.ErrorDataReceived, Sub(s, e)
                                                End Sub
                p.Start()
                ChildJob.Assign(p)
                p.BeginOutputReadLine()
                p.BeginErrorReadLine()
                Try
                    p.StandardInput.Close()
                Catch
                End Try
                If Not p.WaitForExit(TimeoutMs) Then
                    Try
                        p.Kill()
                    Catch
                    End Try
                    problem = "vd_mgr_stale_timeout"
                    Return Nothing
                End If
                p.WaitForExit()
                Dim text As String
                SyncLock gate
                    text = sb.ToString()
                End SyncLock
                If p.ExitCode <> 0 Then
                    ' An older filedo.exe refuses `vd status json` as usage (class 2).
                    problem = If(p.ExitCode = 2 AndAlso Not text.Contains("""schema"""), "vd_mgr_stale_old_cli", "vd_mgr_stale_failed")
                    ShellLog.Info("vd status json ended with " & p.ExitCode.ToString())
                    Return Nothing
                End If
                Return DiskSnapshot.Parse(text, problem)
            End Using
        Catch ex As Exception
            ShellLog.Write("read the disk state", ex)
            problem = "vd_mgr_stale_failed"
            Return Nothing
        End Try
    End Function

End Module
