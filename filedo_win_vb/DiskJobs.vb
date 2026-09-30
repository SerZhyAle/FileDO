Imports System.IO
Imports System.Runtime.InteropServices
Imports System.Text
Imports System.Text.RegularExpressions

' The Disks jobs (SP-0004 P6, spec section 7): what the window knows about a `.fdd` container and
' the console lines it writes for one.
'
' Nothing here parses the container. What the window says about a container - obfuscated or
' encrypted, closed cleanly or not, mounted now or not - is what `filedo.exe <x.fdd> info` printed,
' read line by line (spec 6.4: the console answers, the window does not grow a second reader). And
' nothing here passes a credential on a command line: a password travels in the child's environment
' as FILEDO_SHELL_CRED, a new one for `pass` as FILEDO_SHELL_CRED_NEW, and the line names only the
' variables (spec 7.2).

' Which of the two protections a container has, as `info` said it - or Unknown when nothing has
' been read yet, or the read failed.
Public Enum DiskProtection
    Unknown
    Obfuscated
    Encrypted
End Enum

' What `filedo.exe <x.fdd> info` said about one container.
Public Class ContainerFacts
    Public Property Path As String = ""
    Public Property Read As Boolean = False
    Public Property Protection As DiskProtection = DiskProtection.Unknown
    Public Property Profile As String = ""
    ' Nothing when `info` did not say.
    Public Property Clean As Boolean? = Nothing
    Public Property LastGoodSave As String = ""
    ' "X:" while the container is mounted, "" otherwise.
    Public Property MountedLetter As String = ""

    Public ReadOnly Property IsMounted As Boolean
        Get
            Return MountedLetter <> ""
        End Get
    End Property

    Public Shared Function Unknown(path As String) As ContainerFacts
        Return New ContainerFacts With {.Path = If(path, "")}
    End Function

    ' Reads the lines of vdInfo (cmd/filedo/vdisk_cmd.go). A line this build does not know is
    ' skipped; a container whose protection line is missing is not claimed to be either one.
    Public Shared Function Parse(path As String, output As String, exitCode As Integer) As ContainerFacts
        Dim f = Unknown(path)
        If String.IsNullOrEmpty(output) Then Return f
        For Each raw In output.Split(New String() {vbCrLf, vbLf}, StringSplitOptions.None)
            Dim line = raw.Trim()
            Dim colon = line.IndexOf(":"c)
            If colon <= 0 Then Continue For
            Dim key = line.Substring(0, colon).Trim().ToLowerInvariant()
            Dim value = line.Substring(colon + 1).Trim()
            Select Case key
                Case "protection"
                    ' "obfuscated (obfuscated, not encrypted: ..)" or "encrypted (encrypted)". The
                    ' first word is the protection; "not encrypted" in the note must never read as
                    ' encrypted, so the word is taken, not searched for.
                    Dim first = value.Split(New Char() {" "c, "("c}, StringSplitOptions.RemoveEmptyEntries).FirstOrDefault()
                    Select Case If(first, "").ToLowerInvariant()
                        Case "obfuscated" : f.Protection = DiskProtection.Obfuscated
                        Case "encrypted" : f.Protection = DiskProtection.Encrypted
                    End Select
                Case "profile"
                    f.Profile = value
                Case "closed clean"
                    If value.StartsWith("yes", StringComparison.OrdinalIgnoreCase) Then
                        f.Clean = True
                    ElseIf value.StartsWith("no", StringComparison.OrdinalIgnoreCase) Then
                        f.Clean = False
                    End If
                Case "last good save"
                    f.LastGoodSave = value
                Case "mounted now"
                    If TargetPath.IsDriveToken(value.TrimEnd("\"c)) Then f.MountedLetter = value.TrimEnd("\"c).ToUpperInvariant()
            End Select
        Next
        f.Read = (exitCode = 0 AndAlso f.Protection <> DiskProtection.Unknown)
        Return f
    End Function
End Class

' Everything step 3 of a Disks page answered, as plain values: the page's controls fill it, the
' self-test fills it by hand, and DiskCommands turns it into the console line.
Public Class DiskOptions
    Public Property Size As String = ""
    Public Property Profile As String = "plain"
    Public Property Label As String = ""
    Public Property FileSystem As String = "ntfs"
    Public Property [ReadOnly] As Boolean = False
    Public Property NoScan As Boolean = False
    Public Property Letter As String = ""
    Public Property Force As Boolean = False
    Public Property NoSave As Boolean = False
    Public Property Wipe As Boolean = False
    Public Property NoPass As Boolean = False
    Public Property ExportForm As String = "raw"
    Public Property Dest As String = ""
    Public Property AutoOn As Boolean = True
    Public Property Remember As Boolean = True
    Public Property Name As String = ""
    Public Property ShowMounted As Boolean = False
    Public Property HasCredential As Boolean = False
    Public Property HasNewCredential As Boolean = False
    Public Property Protection As DiskProtection = DiskProtection.Unknown
End Class

Public Module DiskCommands

    ' The variables the credentials travel in. Set on the child process alone, and they die with it.
    Public Const CredentialEnvName As String = "FILEDO_SHELL_CRED"
    Public Const NewCredentialEnvName As String = "FILEDO_SHELL_CRED_NEW"

    Public Const GroupKey As String = "rail_group_disks"

    ' The verbs that read or change a container's contents and so may need its credential.
    Public Function TakesCredential(verb As String) As Boolean
        Select Case verb
            Case "new", "mount", "verify", "export", "compact", "grow", "format", "seal", "clone", "pass"
                Return True
        End Select
        Return False
    End Function

    ' The verbs that act on one existing container named in step 2.
    Public Function ActsOnContainer(verb As String) As Boolean
        Select Case verb
            Case "new", "list" : Return False
        End Select
        Return True
    End Function

    ' The verbs that need the iSCSI transport or elevation, which a packaged build refuses as
    ' class 6 (brief, T6.26).
    Public Function NeedsTransport(verb As String) As Boolean
        Select Case verb
            Case "mount", "unmount", "save", "format", "auto"
                Return True
        End Select
        Return False
    End Function

    ' The verbs refused while the container is mounted (busy, class 8) - checked here as well, so
    ' the page never offers a button the console would refuse (spec 7.4).
    Public Function RefusedWhileMounted(verb As String) As Boolean
        Select Case verb
            Case "format", "destroy", "compact", "grow", "pass"
                Return True
        End Select
        Return False
    End Function

    ' The console line for one Disks page, in the grammar of the P6 brief. The credential goes by
    ' variable name only; an empty one on `new` is the visible `p:`, which is the documented choice
    ' of an obfuscated container (the console prints the obfuscation sentence for it).
    Public Function Build(verb As String, target As String, o As DiskOptions) As List(Of String)
        Dim a As New List(Of String)()
        Dim hasTarget = Not String.IsNullOrEmpty(target)
        If o Is Nothing Then o = New DiskOptions()

        Select Case verb
            Case "list"
                ' The machine-readable snapshot (SP-0063 8.1), whichever table is asked for: the
                ' registered containers and what is mounted are two views of the one document, so
                ' a reworded console line can never empty a column.
                a.Add("vd")
                a.Add("status")
                a.Add("json")
                Return a

            Case "new"
                a.Add("vd")
                a.Add("new")
                If hasTarget Then a.Add(target)
                If o.Size.Trim() <> "" Then a.Add(o.Size.Trim())
                a.Add(o.Profile)
                If o.Label.Trim() <> "" Then
                    a.Add("label")
                    a.Add(o.Label.Trim())
                End If
                a.Add(If(o.HasCredential, "pe:" & CredentialEnvName, "p:"))
                Return a

            Case "auto"
                a.Add("vd")
                a.Add("auto")
                If o.AutoOn Then
                    If hasTarget Then a.Add(target)
                    a.Add("logon")
                Else
                    a.Add("off")
                    If hasTarget Then a.Add(target)
                End If
                Return a

            Case "add"
                a.Add("vd")
                If o.Remember Then
                    a.Add("add")
                    If hasTarget Then a.Add(target)
                    If o.Name.Trim() <> "" Then
                        a.Add("as")
                        a.Add(o.Name.Trim())
                    End If
                Else
                    a.Add("forget")
                    If hasTarget Then a.Add(target)
                End If
                Return a
        End Select

        ' Target-first: filedo <file.fdd> <verb> ..
        If hasTarget Then a.Add(target)
        a.Add(verb)
        Select Case verb
            Case "mount"
                If o.ReadOnly Then a.Add("ro")
                If o.NoScan Then a.Add("noscan")
                If o.Letter <> "" Then
                    a.Add("as")
                    a.Add(o.Letter)
                End If
            Case "unmount"
                ' `nosave` asks a question on the console, and `force` is the only answer to it
                ' this window can give - the page asked it first, as the typed word.
                If o.Force OrElse o.NoSave Then a.Add("force")
                If o.NoSave Then a.Add("nosave")
            Case "export"
                If o.Dest.Trim() <> "" Then a.Add(o.Dest.Trim())
                a.Add(If(o.ExportForm = "vhd", "vhd", "raw"))
            Case "grow"
                If o.Size.Trim() <> "" Then a.Add(o.Size.Trim())
            Case "format"
                a.Add("fs")
                a.Add(If(o.FileSystem = "exfat", "exfat", "ntfs"))
                If o.Label.Trim() <> "" Then
                    a.Add("label")
                    a.Add(o.Label.Trim())
                End If
                ' The prompt was answered on the page as the typed FORMAT; force skips the prompt
                ' and never the check (a mounted container is still refused).
                a.Add("force")
            Case "seal", "clone"
                If o.Dest.Trim() <> "" Then a.Add(o.Dest.Trim())
                If o.NoPass Then a.Add("nopass")
            Case "destroy"
                If o.Wipe Then a.Add("wipe")
                a.Add("force")
            Case "pass"
                If o.HasCredential Then a.Add("pe:" & CredentialEnvName)
                a.Add("new")
                a.Add("pe:" & NewCredentialEnvName)
                Return a
        End Select

        ' An obfuscated container opens without a credential, and one given is only reported as
        ' not used - so none is sent. Otherwise a typed one goes by name; an empty one is not sent
        ' at all, and the console, finding no terminal, refuses rather than hangs.
        If TakesCredential(verb) AndAlso o.Protection <> DiskProtection.Obfuscated AndAlso o.HasCredential Then
            a.Add("pe:" & CredentialEnvName)
        End If
        Return a
    End Function

    ' A size the console's parseSize takes: a number, optionally with a unit (20G, 512M, 1.5T).
    Private ReadOnly SizePattern As New Regex("^\d+(\.\d+)?\s*([kmgt]i?b?)?$", RegexOptions.IgnoreCase Or RegexOptions.CultureInvariant)

    Public Function IsSize(text As String) As Boolean
        If String.IsNullOrWhiteSpace(text) Then Return False
        Return SizePattern.IsMatch(text.Trim())
    End Function

    Public Function IsFddPath(path As String) As Boolean
        Return Not String.IsNullOrEmpty(path) AndAlso path.Trim().EndsWith(".fdd", StringComparison.OrdinalIgnoreCase)
    End Function

    ' A file dialog's filter. The "|" it needs cannot live in a translation (GetDict splits on it),
    ' so the translated names are joined here.
    Public Function FileFilter(name As String, pattern As String, allName As String) As String
        Return name & "|" & pattern & "|" & allName & "|*.*"
    End Function

    ' The sentence for a container verb's exit class (brief: 2 usage .. 8 busy), or "" for none.
    Public Function ExitKey(code As Integer) As String
        Select Case code
            Case 2, 3, 4, 5, 6, 7, 8 : Return "vd_exit_" & code.ToString()
        End Select
        Return ""
    End Function

    ' The drive letter a mount printed ("Mounted at X:." / "Mounted read-only at X:.").
    Private ReadOnly MountedLine As New Regex("^Mounted\b.*?\bat ([A-Za-z]:)", RegexOptions.CultureInvariant Or RegexOptions.Multiline)

    Public Function MountedLetterIn(output As String) As String
        If String.IsNullOrEmpty(output) Then Return ""
        Dim m = MountedLine.Match(output)
        Return If(m.Success, m.Groups(1).Value.ToUpperInvariant(), "")
    End Function

End Module

' Asks filedo.exe about one container, off the window's thread: `filedo.exe --no-history <x.fdd>
' info`, which reads the header and needs no credential. The child is started the way every run is
' (Runner.NewStartInfo): no window, stdin closed, in the window's job.
Public Module DiskProbe

    ' The self-test never starts filedo.exe; it turns this off before it builds a page.
    Friend Enabled As Boolean = True

    Private Const TimeoutMs As Integer = 8000

    Public Function Info(path As String) As ContainerFacts
        If Not Enabled OrElse String.IsNullOrEmpty(path) Then Return ContainerFacts.Unknown(path)
        Try
            If Not File.Exists(path) Then Return ContainerFacts.Unknown(path)
            Dim psi = Runner.NewStartInfo(Runner.LocateCLI(),
                                          ArgQuoting.JoinArgs(New String() {"--no-history", path, "info"}),
                                          Runner.GetAppDataDir(), False)
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
                    Return ContainerFacts.Unknown(path)
                End If
                p.WaitForExit()
                SyncLock gate
                    Return ContainerFacts.Parse(path, sb.ToString(), p.ExitCode)
                End SyncLock
            End Using
        Catch ex As Exception
            ShellLog.Write("read the container's header", ex)
            Return ContainerFacts.Unknown(path)
        End Try
    End Function

End Module

' Whether this process runs inside an MSIX package (the Microsoft Store build). Such a build cannot
' attach a disk, and the console refuses every transport verb there as class 6 (T6.26); the window
' says so before it offers a mount (T6.25).
Public Module Packaging

    <DllImport("kernel32.dll", CharSet:=CharSet.Unicode)>
    Private Function GetCurrentPackageFullName(ByRef length As Integer, name As StringBuilder) As Integer
    End Function

    Private Const APPMODEL_ERROR_NO_PACKAGE As Integer = 15700

    ' The self-test sets this to see both answers.
    Friend OverrideForTest As Boolean? = Nothing

    Private cached As Boolean? = Nothing

    Public Function IsPackaged() As Boolean
        If OverrideForTest.HasValue Then Return OverrideForTest.Value
        If cached.HasValue Then Return cached.Value
        Dim result = False
        Try
            Dim length = 0
            Dim rc = GetCurrentPackageFullName(length, Nothing)
            result = (rc <> APPMODEL_ERROR_NO_PACKAGE)
        Catch
            ' Windows 7 has no such function, and no packages either.
            result = False
        End Try
        cached = result
        Return result
    End Function

End Module

' How a start on a `.fdd` is answered (spec 6.4, T6.18). `filedo_win.exe "<x.fdd>"` mounts,
' `--mount-ro "<x.fdd>"` mounts read-only, `--unmount "<x.fdd>"` unmounts. A `.fdd` never opens the
' secure page. The decision is a function of what `info` said, so the self-test can hold all three
' routes without a container or a filedo.exe.
Public Module DiskRoute

    Public Enum StartMode
        None
        Mount
        MountReadOnly
        Unmount
    End Enum

    Public Enum RouteAction
        ' Open the drive letter in Explorer; no window.
        OpenDrive
        ' Open a Disks page on the container.
        OpenPage
    End Enum

    Public Class Decision
        Public Property Action As RouteAction = RouteAction.OpenPage
        Public Property JobKey As String = "rail_job_vd_mount"
        Public Property Path As String = ""
        Public Property Letter As String = ""
        Public Property [ReadOnly] As Boolean = False
        ' Run the page's command straight away: nothing is left to ask.
        Public Property AutoRun As Boolean = False
        Public Property Facts As ContainerFacts
    End Class

    ' The switch a start carries, if any. Double-dash, like the product's others.
    Public Function ModeFrom(args As String()) As StartMode
        If args Is Nothing Then Return StartMode.None
        For Each a In args
            Select Case If(a, "").ToLowerInvariant()
                Case "--mount-ro" : Return StartMode.MountReadOnly
                Case "--unmount" : Return StartMode.Unmount
                Case "--mount" : Return StartMode.Mount
            End Select
        Next
        Return StartMode.None
    End Function

    Public Function Decide(path As String, mode As StartMode, facts As ContainerFacts, packaged As Boolean) As Decision
        If facts Is Nothing Then facts = ContainerFacts.Unknown(path)
        Dim d As New Decision With {.Path = path, .Facts = facts}

        If mode = StartMode.Unmount Then
            d.JobKey = "rail_job_vd_unmount"
            ' An unmount of what is mounted asks nothing more; one of what is not mounted, or of a
            ' container that could not be read, stops on the page and says why.
            d.AutoRun = facts.Read AndAlso facts.IsMounted AndAlso Not packaged
            Return d
        End If

        ' What a user means by opening a mounted container is its drive (spec 6.4).
        If facts.IsMounted Then
            d.Action = RouteAction.OpenDrive
            d.Letter = facts.MountedLetter
            Return d
        End If

        d.JobKey = "rail_job_vd_mount"
        d.ReadOnly = (mode = StartMode.MountReadOnly)
        ' The mount runs at once only when there is nothing to ask: the container was read, it was
        ' closed cleanly, it needs no password, and this build can mount. An unclean container is
        ' reported on the page before anything is attached, and the page waits for the user.
        d.AutoRun = facts.Read AndAlso facts.Clean.HasValue AndAlso facts.Clean.Value AndAlso
                    facts.Protection = DiskProtection.Obfuscated AndAlso Not packaged
        Return d
    End Function

    Public Function IsContainer(path As String) As Boolean
        Return DiskCommands.IsFddPath(path)
    End Function

End Module
