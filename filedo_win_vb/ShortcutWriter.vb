Imports System.IO
Imports System.Runtime.InteropServices
Imports System.Text

' Desktop shortcuts belong to the user. This unit never runs the saved command.
Friend Module ShortcutWriter
    Private Const ShellLinkClsid As String = "00021401-0000-0000-C000-000000000046"

    <ComImport, Guid("000214F9-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)>
    Private Interface IShellLinkW
        Sub GetPath(<Out, MarshalAs(UnmanagedType.LPWStr)> path As StringBuilder, maxPath As Integer, ByRef data As WIN32_FIND_DATAW, flags As UInteger)
        Sub GetIDList(ByRef pidl As IntPtr)
        Sub SetIDList(pidl As IntPtr)
        Sub GetDescription(<Out, MarshalAs(UnmanagedType.LPWStr)> description As StringBuilder, maxLength As Integer)
        Sub SetDescription(<MarshalAs(UnmanagedType.LPWStr)> description As String)
        Sub GetWorkingDirectory(<Out, MarshalAs(UnmanagedType.LPWStr)> directory As StringBuilder, maxLength As Integer)
        Sub SetWorkingDirectory(<MarshalAs(UnmanagedType.LPWStr)> directory As String)
        Sub GetArguments(<Out, MarshalAs(UnmanagedType.LPWStr)> arguments As StringBuilder, maxLength As Integer)
        Sub SetArguments(<MarshalAs(UnmanagedType.LPWStr)> arguments As String)
        Sub GetHotkey(ByRef hotkey As Short)
        Sub SetHotkey(hotkey As Short)
        Sub GetShowCmd(ByRef show As Integer)
        Sub SetShowCmd(show As Integer)
        Sub GetIconLocation(<Out, MarshalAs(UnmanagedType.LPWStr)> icon As StringBuilder, maxLength As Integer, ByRef index As Integer)
        Sub SetIconLocation(<MarshalAs(UnmanagedType.LPWStr)> icon As String, index As Integer)
        Sub SetRelativePath(<MarshalAs(UnmanagedType.LPWStr)> path As String, reserved As UInteger)
        Sub Resolve(hwnd As IntPtr, flags As UInteger)
        Sub SetPath(<MarshalAs(UnmanagedType.LPWStr)> path As String)
    End Interface

    <StructLayout(LayoutKind.Sequential, CharSet:=CharSet.Unicode)>
    Private Structure WIN32_FIND_DATAW
        Public attributes As UInteger
        Public creationLow As UInteger
        Public creationHigh As UInteger
        Public accessLow As UInteger
        Public accessHigh As UInteger
        Public writeLow As UInteger
        Public writeHigh As UInteger
        Public sizeHigh As UInteger
        Public sizeLow As UInteger
        Public reserved0 As UInteger
        Public reserved1 As UInteger
        <MarshalAs(UnmanagedType.ByValTStr, SizeConst:=260)> Public fileName As String
        <MarshalAs(UnmanagedType.ByValTStr, SizeConst:=14)> Public alternateName As String
    End Structure

    <ComImport, Guid("0000010b-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)>
    Private Interface IPersistFile
        Sub GetClassID(ByRef classId As Guid)
        <PreserveSig> Function IsDirty() As Integer
        Sub Load(<MarshalAs(UnmanagedType.LPWStr)> fileName As String, mode As UInteger)
        Sub Save(<MarshalAs(UnmanagedType.LPWStr)> fileName As String, remember As Boolean)
        Sub SaveCompleted(<MarshalAs(UnmanagedType.LPWStr)> fileName As String)
        Sub GetCurFile(<Out, MarshalAs(UnmanagedType.LPWStr)> ByRef fileName As String)
    End Interface

    Friend Function ParseLine(line As String, ByRef args As List(Of String)) As String
        args = New List(Of String)()
        Dim cmd = If(line, "").Trim()
        If cmd.Equals("filedo.exe", StringComparison.OrdinalIgnoreCase) Then Return "shortcut_empty"
        If cmd.StartsWith("filedo.exe ", StringComparison.OrdinalIgnoreCase) Then cmd = cmd.Substring(11).Trim()
        If cmd = "" Then Return "shortcut_empty"
        ' SplitArgs intentionally tolerates an unfinished quote for the editable command box.
        ' A persisted shortcut needs an unambiguous Windows command line.
        Dim quoted = False
        Dim slashes = 0
        For i = 0 To cmd.Length - 1
            If cmd(i) = "\"c Then
                slashes += 1
            Else
                If cmd(i) = """"c AndAlso slashes Mod 2 = 0 Then
                    If quoted AndAlso i + 1 < cmd.Length AndAlso cmd(i + 1) = """"c Then
                        i += 1
                    Else
                        quoted = Not quoted
                    End If
                End If
                slashes = 0
            End If
        Next
        If quoted Then Return "shortcut_parse"
        Try
            args = ArgQuoting.SplitArgs(cmd)
        Catch
            Return "shortcut_parse"
        End Try
        If args.Count = 0 Then Return "shortcut_empty"
        For Each arg In args
            If arg.Any(Function(ch) ch = ChrW(0) OrElse ch = ControlChars.Lf OrElse ch = ControlChars.Cr) Then Return "shortcut_parse"
            If arg.StartsWith("p:", StringComparison.OrdinalIgnoreCase) Then Return "shortcut_password"
            If arg.Equals("pe:FILEDO_SHELL_CRED", StringComparison.OrdinalIgnoreCase) Then Return "shortcut_shell_cred"
        Next
        Return ""
    End Function

    Friend Function HasPause(args As IEnumerable(Of String)) As Boolean
        Return args.Any(Function(a) a.Equals("--pause", StringComparison.OrdinalIgnoreCase))
    End Function

    Friend Function SavedArgs(args As IEnumerable(Of String), keepOpen As Boolean) As List(Of String)
        Dim result As New List(Of String)(args)
        If keepOpen AndAlso Not HasPause(result) Then result.Add("--pause")
        Return result
    End Function

    Friend Function CleanName(value As String) As String
        Dim b As New StringBuilder()
        For Each ch In If(value, "")
            If ch < " "c OrElse "<>:""/\|?*".IndexOf(ch) >= 0 Then
                b.Append("-"c)
            Else
                b.Append(ch)
            End If
        Next
        Dim result = b.ToString().Trim().TrimEnd("."c, " "c)
        If result.Length > 100 Then result = result.Substring(0, 100).TrimEnd("."c, " "c)
        If result = "" Then Return "FileDO command"
        Dim stem = result.Split("."c)(0).ToUpperInvariant()
        If stem = "CON" OrElse stem = "PRN" OrElse stem = "AUX" OrElse stem = "NUL" OrElse
           System.Text.RegularExpressions.Regex.IsMatch(stem, "^(COM|LPT)[1-9]$") Then result = "FileDO - " & result
        Return result
    End Function

    Friend Function DefaultName(args As IList(Of String)) As String
        Dim words = args.Where(Function(a) Not a.StartsWith("--", StringComparison.Ordinal)).Take(2).ToArray()
        If words.Length = 2 AndAlso (words(0).Contains("\") OrElse words(0).Contains("/") OrElse
                                      words(0).EndsWith(":")) Then
            Return CleanName("FileDO - " & words(1) & " " & words(0))
        End If
        Return CleanName("FileDO - " & String.Join(" ", words))
    End Function

    Friend Function AvailablePath(folder As String, name As String) As String
        Dim stem = CleanName(name)
        Dim candidate = Path.Combine(folder, stem & ".lnk")
        Dim n = 2
        While File.Exists(candidate) OrElse Directory.Exists(candidate)
            candidate = Path.Combine(folder, stem & " (" & n.ToString() & ").lnk")
            n += 1
        End While
        Return candidate
    End Function

    Friend Function ResolveExe() As String
        Dim exe = Runner.LocateCLI()
        If Not Path.IsPathRooted(exe) OrElse Not File.Exists(exe) Then Return ""
        If exe.IndexOf("\windowsapps\", StringComparison.OrdinalIgnoreCase) < 0 Then Return Path.GetFullPath(exe)
        Dim aliasPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                                     "Microsoft", "WindowsApps", "filedo.exe")
        If File.Exists(aliasPath) Then Return aliasPath
        Return ""
    End Function

    Friend Function Create(folder As String, name As String, target As String, arguments As String,
                           workingDirectory As String, icon As String) As String
        Dim destination = AvailablePath(folder, name)
        Dim temporary = Path.Combine(folder, ".filedo-" & Guid.NewGuid().ToString("N") & ".lnk")
        Dim link As Object = Nothing
        Try
            link = Activator.CreateInstance(Type.GetTypeFromCLSID(New Guid(ShellLinkClsid)))
            Dim shell = DirectCast(link, IShellLinkW)
            shell.SetPath(target)
            shell.SetArguments(arguments)
            shell.SetWorkingDirectory(workingDirectory)
            shell.SetIconLocation(icon, 0)
            shell.SetShowCmd(1)
            DirectCast(link, IPersistFile).Save(temporary, True)
            ' File.Move never replaces a user's existing shortcut. A collision at this point
            ' is reported to the dialog and leaves the existing file untouched.
            File.Move(temporary, destination)
            Return destination
        Finally
            If link IsNot Nothing Then Marshal.FinalReleaseComObject(link)
            If File.Exists(temporary) Then File.Delete(temporary)
        End Try
    End Function

    Friend Function ReadBack(path As String) As String()
        Dim link As Object = Nothing
        Try
            link = Activator.CreateInstance(Type.GetTypeFromCLSID(New Guid(ShellLinkClsid)))
            DirectCast(link, IPersistFile).Load(path, 0)
            Dim shell = DirectCast(link, IShellLinkW)
            Dim target As New StringBuilder(32768)
            Dim args As New StringBuilder(32768)
            Dim directory As New StringBuilder(32768)
            Dim icon As New StringBuilder(32768)
            Dim data As New WIN32_FIND_DATAW()
            Dim iconIndex = 0
            shell.GetPath(target, target.Capacity, data, 4UI)
            shell.GetArguments(args, args.Capacity)
            shell.GetWorkingDirectory(directory, directory.Capacity)
            shell.GetIconLocation(icon, icon.Capacity, iconIndex)
            Return New String() {target.ToString(), args.ToString(), directory.ToString(), icon.ToString()}
        Finally
            If link IsNot Nothing Then Marshal.FinalReleaseComObject(link)
        End Try
    End Function
End Module

Friend Class ShortcutDialog
    Inherits Form

    Private ReadOnly args As List(Of String)
    Private ReadOnly target As String
    Private ReadOnly working As String
    Private ReadOnly sourceFolder As String
    Private ReadOnly nameBox As TextBox
    Private ReadOnly pauseBox As CheckBox
    Private ReadOnly preview As Label
    Private ReadOnly feedback As Label
    Private ReadOnly createButton As Button
    Private ReadOnly cancelButtonValue As Button
    Private ReadOnly showLink As LinkLabel
    Private createdPath As String = ""
    Private ReadOnly labels As Dictionary(Of String, String)

    Private Function L(key As String) As String
        Return labels(key)
    End Function

    Friend Shared Sub ShowFor(owner As IWin32Window, line As String, wipes As Boolean)
        Dim args As List(Of String) = Nothing
        Dim reason = ShortcutWriter.ParseLine(line, args)
        If reason <> "" Then
            ShellDialog.Notice(owner, Localization.T("shortcut_title"), Localization.T(reason))
            Return
        End If
        Dim exe = ShortcutWriter.ResolveExe()
        If exe = "" Then
            ShellDialog.Notice(owner, Localization.T("shortcut_title"), Localization.T("shortcut_no_exe"))
            Return
        End If
        Using dlg As New ShortcutDialog(args, exe, wipes)
            dlg.ShowDialog(owner)
        End Using
    End Sub

    Private Sub New(arguments As List(Of String), exe As String, wipes As Boolean)
        args = arguments
        target = exe
        sourceFolder = Path.GetDirectoryName(Runner.LocateCLI())
        working = Runner.GetAppDataDir()
        labels = Localization.GetDict(ShellSettings.Language())
        Text = L("shortcut_title")
        StartPosition = FormStartPosition.CenterParent
        FormBorderStyle = FormBorderStyle.FixedDialog
        MinimizeBox = False
        MaximizeBox = False
        ShowInTaskbar = False
        ShowIcon = False
        AutoScaleMode = AutoScaleMode.Font
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        Font = Theme.FontBody()

        Dim root As New TableLayoutPanel With {.ColumnCount = 1, .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Dock = DockStyle.Fill,
            .Padding = Ui.PxPad(Me, 20, 18, 20, 14)}
        root.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        Dim nameLabel As New Label With {.Text = L("shortcut_name"), .AutoSize = True}
        root.Controls.Add(nameLabel)
        nameBox = New TextBox With {.Text = ShortcutWriter.DefaultName(args), .Width = Ui.Px(Me, 440),
            .AccessibleName = L("shortcut_name")}
        root.Controls.Add(nameBox)
        pauseBox = New CheckBox With {.Text = L("shortcut_pause"), .Checked = True,
            .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 12, 0, 8)}
        root.Controls.Add(pauseBox)
        preview = New Label With {.AutoSize = True, .MaximumSize = New Size(Ui.Px(Me, 500), 0)}
        root.Controls.Add(preview)
        If wipes Then
            Dim wipeTarget = If(args.Count > 0, args(0), "")
            For i = 1 To args.Count - 1
                If WipeSafety.IsWipeAlias(args(i)) Then
                    wipeTarget = args(i - 1)
                    Exit For
                End If
            Next
            Dim warning As New Label With {.Text = Localization.Format(L("shortcut_wipe"), wipeTarget), .AutoSize = True,
                .MaximumSize = New Size(Ui.Px(Me, 500), 0), .ForeColor = Theme.Current.Danger}
            root.Controls.Add(warning)
        End If
        feedback = New Label With {.AutoSize = True, .MaximumSize = New Size(Ui.Px(Me, 500), 0),
            .Margin = Ui.PxPad(Me, 0, 10, 0, 6)}
        root.Controls.Add(feedback)
        showLink = New LinkLabel With {.Text = L("shortcut_show"), .AutoSize = True, .Visible = False}
        AddHandler showLink.LinkClicked, AddressOf ShowCreated
        root.Controls.Add(showLink)
        Dim buttons As New FlowLayoutPanel With {.AutoSize = True, .WrapContents = False,
            .FlowDirection = FlowDirection.RightToLeft, .Anchor = AnchorStyles.Right}
        cancelButtonValue = New Button With {.Text = L("shortcut_cancel"), .AutoSize = True}
        createButton = New Button With {.Text = L("shortcut_create"), .AutoSize = True}
        AddHandler cancelButtonValue.Click, Sub() Close()
        AddHandler createButton.Click, AddressOf CreateClicked
        buttons.Controls.Add(cancelButtonValue)
        buttons.Controls.Add(createButton)
        root.Controls.Add(buttons)
        Controls.Add(root)
        AcceptButton = createButton
        CancelButton = cancelButtonValue
        AddHandler pauseBox.CheckedChanged, Sub() UpdatePreview()
        UpdatePreview()
        ApplyTheme()
        Theme.Watch(Me, AddressOf ApplyTheme)
    End Sub

    Private Sub UpdatePreview()
        preview.Text = Localization.Format(L("shortcut_preview"), "filedo.exe " &
                                           ArgQuoting.JoinArgs(ShortcutWriter.SavedArgs(args, pauseBox.Checked)))
    End Sub

    Private Sub CreateClicked(sender As Object, e As EventArgs)
        Dim desktop = Environment.GetFolderPath(Environment.SpecialFolder.Desktop)
        If Not Directory.Exists(desktop) Then
            feedback.Text = L("shortcut_no_desktop")
            Return
        End If
        Dim name = ShortcutWriter.CleanName(nameBox.Text)
        If String.IsNullOrWhiteSpace(nameBox.Text) Then
            feedback.Text = L("shortcut_need_name")
            Return
        End If
        Dim iconCandidate = Path.Combine(sourceFolder, "FileDO.ico")
        Dim icon = If(File.Exists(iconCandidate), iconCandidate, target)
        Try
            createdPath = ShortcutWriter.Create(desktop, name, target,
                                                ArgQuoting.JoinArgs(ShortcutWriter.SavedArgs(args, pauseBox.Checked)), working, icon)
            feedback.Text = Localization.Format(L("shortcut_success"), Path.GetFileName(createdPath)) &
                            Environment.NewLine & L("shortcut_uninstall")
            showLink.Visible = True
            createButton.Enabled = False
        Catch ex As Exception
            ShellLog.Write("create desktop shortcut", ex)
            feedback.Text = If(TypeOf ex Is IOException, L("shortcut_collision"), L("shortcut_failed"))
        End Try
    End Sub

    Private Sub ShowCreated(sender As Object, e As LinkLabelLinkClickedEventArgs)
        If createdPath = "" Then Return
        Try
            Process.Start("explorer.exe", "/select," & ArgQuoting.EscapeArg(createdPath))
        Catch ex As Exception
            ShellLog.Write("show desktop shortcut", ex)
            feedback.Text = L("shortcut_show_failed")
        End Try
    End Sub

    Private Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text
        Ui.HitTargetFloor(Me)
        nameBox.BackColor = p.SurfaceAlt
        nameBox.ForeColor = p.Text
        nameBox.Font = Theme.FontBody()
        preview.ForeColor = p.Text
        preview.Font = Theme.FontMono()
        feedback.ForeColor = p.MutedText
        showLink.LinkColor = p.Accent
        showLink.ActiveLinkColor = p.Accent
        Ui.StyleButton(createButton, p.Accent, p.AccentText, p.Accent)
        Ui.StyleButton(cancelButtonValue, p.SurfaceAlt, p.Text, p.Border)
    End Sub

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
    End Sub
End Class
