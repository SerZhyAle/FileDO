Imports System.IO
Imports System.Web.Script.Serialization

' In-app help (SP-0168; contract CONTEXT-HELP 0.1): one resolver and one map.
'
' A surface is a window, a page of the shell or a dialog. The help map (docs/help/help-map.json, the
' same file the package ships beside the exe) says for each one whether it has help of its own
' (`page`), borrows its parent's (`inherit`) or has none (`none`), and always says why for the last
' two. F1 and the `?` button of every help-bearing surface both call Open below, so the key and the
' button cannot give different answers (rule 3), and no window carries help code of its own (rule 17).
'
' The answer is a page: an HTML file in help\<language>\ beside the exe, opened in the system browser
' through Links.Open on a click or a key, and never otherwise. The page is chosen by the
' application's language, falls back to English and never to a missing page (rule 13). A page that is
' not written yet falls back to the page its map row names, so a surface of a later slice still
' answers with the part of the help that does explain it.
'
' The page is complete without the application running and makes no request. The theme is the one
' thing it cannot know, so it is passed in the address (`?theme=dark`), and a page opened without it
' follows the system.
Friend Class HelpSurface
    Friend Id As String = ""
    ' The surface's name as the UI shows it: Name in English, NameKey the string it is read from.
    Friend Name As String = ""
    Friend NameKey As String = ""
    ' layer, page, both, inherit or none (rule 4).
    Friend Form As String = ""
    Friend HelpId As String = ""
    Friend Parent As String = ""
    Friend Reason As String = ""
    ' What the surface is in the source: the rail key of a destination, the class of a window.
    Friend Rail As String = ""
    Friend ClassName As String = ""
    ' The help id shown while this surface's own page is not written yet.
    Friend Fallback As String = ""
End Class

Friend Class HelpTarget
    Friend SurfaceId As String = ""
    Friend HelpId As String = ""
    Friend Anchor As String = ""
End Class

Friend Class HelpMap
    Friend ReadOnly Surfaces As New List(Of HelpSurface)
    Friend SchemaVersion As Integer = 0
    Friend Product As String = ""
    Friend Index As String = "index"

    Friend Function ById(id As String) As HelpSurface
        Return Surfaces.FirstOrDefault(Function(s) s.Id = id)
    End Function

    Friend Function ByRail(key As String) As HelpSurface
        Return Surfaces.FirstOrDefault(Function(s) s.Rail = key)
    End Function

    ' Nothing for a file that is not a map. The reader ignores members it does not know, so a later
    ' shape can be read by this code (rule 17, section 3 of the contract).
    Friend Shared Function Parse(json As String) As HelpMap
        Try
            Dim js As New JavaScriptSerializer() With {.MaxJsonLength = Integer.MaxValue}
            Dim root = TryCast(js.DeserializeObject(json), Dictionary(Of String, Object))
            If root Is Nothing Then Return Nothing
            Dim map As New HelpMap()
            map.SchemaVersion = IntOf(root, "schemaVersion")
            map.Product = StrOf(root, "product")
            Dim idx = StrOf(root, "index")
            If idx <> "" Then map.Index = idx
            Dim rows = TryCast(If(root.ContainsKey("surfaces"), root("surfaces"), Nothing), IEnumerable)
            If rows Is Nothing Then Return Nothing
            For Each row In rows
                Dim d = TryCast(row, Dictionary(Of String, Object))
                If d Is Nothing Then Continue For
                map.Surfaces.Add(New HelpSurface With {
                    .Id = StrOf(d, "id"), .Name = StrOf(d, "name"), .NameKey = StrOf(d, "nameKey"),
                    .Form = StrOf(d, "form"), .HelpId = StrOf(d, "help"), .Parent = StrOf(d, "parent"),
                    .Reason = StrOf(d, "reason"), .Rail = StrOf(d, "rail"), .ClassName = StrOf(d, "class"),
                    .Fallback = StrOf(d, "fallback")})
            Next
            Return map
        Catch ex As Exception
            ShellLog.Write("help map", ex)
            Return Nothing
        End Try
    End Function

    Private Shared Function StrOf(d As Dictionary(Of String, Object), key As String) As String
        Dim v As Object = Nothing
        If d.TryGetValue(key, v) AndAlso v IsNot Nothing Then Return v.ToString()
        Return ""
    End Function

    Private Shared Function IntOf(d As Dictionary(Of String, Object), key As String) As Integer
        Dim n As Integer
        If Integer.TryParse(StrOf(d, key), n) Then Return n
        Return 0
    End Function
End Class

Friend Module Help
    Friend Const IndexId As String = "index"
    Friend Const MapFile As String = "help-map.json"

    Private mapValue As HelpMap = Nothing
    Private mapLoaded As Boolean = False

    ' For SelfTest.vb: a root and a map of its own, so a test never reads what is installed.
    Friend RootForTest As String = Nothing

    ' The folder the pages are read from: help\ beside the exe, or - for a build run from the source
    ' tree, where nothing has been copied yet - the repository's docs\help\ above it. "" when neither
    ' exists; the page of the site answers then.
    Friend Function Root() As String
        If RootForTest IsNot Nothing Then Return RootForTest
        Try
            Dim beside = Path.Combine(AppContext.BaseDirectory, "help")
            If File.Exists(Path.Combine(beside, MapFile)) Then Return beside
            Dim dir As DirectoryInfo = New DirectoryInfo(AppContext.BaseDirectory)
            For i = 1 To 6
                dir = dir.Parent
                If dir Is Nothing Then Exit For
                Dim candidate = Path.Combine(dir.FullName, "docs", "help")
                If File.Exists(Path.Combine(candidate, MapFile)) Then Return candidate
            Next
        Catch ex As Exception
            ShellLog.Write("help root", ex)
        End Try
        Return ""
    End Function

    Friend Function Map() As HelpMap
        If Not mapLoaded Then
            mapLoaded = True
            Dim root = Help.Root()
            If root <> "" Then
                Try
                    mapValue = HelpMap.Parse(File.ReadAllText(Path.Combine(root, MapFile)))
                Catch ex As Exception
                    ShellLog.Write("help map read", ex)
                End Try
            End If
        End If
        Return mapValue
    End Function

    Friend Sub ResetForTest()
        mapLoaded = False
        mapValue = Nothing
    End Sub

    ' ---- the resolver (rule 17) --------------------------------------------

    ' The surface a destination of the shell is, by its rail key (rail_job_wipe -> shell.wipe).
    Friend Function ShellSurfaceId(railKey As String) As String
        Const prefix As String = "rail_job_"
        If railKey IsNot Nothing AndAlso railKey.StartsWith(prefix, StringComparison.Ordinal) Then
            Return "shell." & railKey.Substring(prefix.Length)
        End If
        Return "shell"
    End Function

    ' Surface id (and optionally an element of it) -> where the answer is. A surface that inherits is
    ' answered by its parent, which is how a dialog of a surface that has help gets it. Never Nothing:
    ' an id the map does not know, a map that cannot be read and a surface with no help all end at
    ' the index, which exists in every language.
    Friend Function Resolve(surfaceId As String, Optional elementId As String = Nothing) As HelpTarget
        Dim m = Map()
        Dim s As HelpSurface = If(m Is Nothing, Nothing, m.ById(surfaceId))
        Dim hops = 0
        While s IsNot Nothing AndAlso s.Form = "inherit" AndAlso s.Parent <> "" AndAlso hops < 8
            s = m.ById(s.Parent)
            hops += 1
        End While
        If s Is Nothing OrElse s.HelpId = "" OrElse s.Form = "none" OrElse s.Form = "inherit" Then
            Return New HelpTarget With {.SurfaceId = surfaceId, .HelpId = IndexId}
        End If
        Dim helpId = s.HelpId
        Dim lang = ShellSettings.Language()
        If Not PageExists(helpId, lang) AndAlso s.Fallback <> "" Then helpId = s.Fallback
        Dim anchor = ""
        If Not String.IsNullOrEmpty(elementId) AndAlso HelpContent.HasElement(helpId, elementId) Then
            anchor = HelpContent.AnchorOf(elementId)
        End If
        Return New HelpTarget With {.SurfaceId = s.Id, .HelpId = helpId, .Anchor = anchor}
    End Function

    Friend Function IndexTarget() As HelpTarget
        Return New HelpTarget With {.HelpId = IndexId}
    End Function

    ' The control that has the keyboard focus in a window, descending through the containers (a view
    ' is a container of its own, and the window's ActiveControl stops at it).
    Friend Function FocusedControl(form As ContainerControl) As Control
        Dim c As Control = form.ActiveControl
        While True
            Dim cc = TryCast(c, ContainerControl)
            If cc Is Nothing OrElse cc.ActiveControl Is Nothing Then Exit While
            c = cc.ActiveControl
        End While
        Return c
    End Function

    ' The element a keyboard focus is in, read from the names the views give their parts
    ' ("help:<id>" on a control or a container, "setting:<id>" on a settings row): the nearest
    ' named ancestor wins. "" when the focus is on nothing the help has an entry for.
    Friend Function ElementOf(c As Control) As String
        While c IsNot Nothing
            Dim n = c.Name
            If n IsNot Nothing Then
                If n.StartsWith("help:", StringComparison.Ordinal) Then Return n.Substring(5)
                If n.StartsWith("setting:", StringComparison.Ordinal) Then Return "setting-" & n.Substring(8)
                If n.StartsWith("settings-group:", StringComparison.Ordinal) Then
                    Dim id = n.Substring(15)
                    Dim colon = id.IndexOf(":"c)
                    Return "group-" & If(colon > 0, id.Substring(0, colon), id)
                End If
            End If
            c = c.Parent
        End While
        Return ""
    End Function

    ' ---- the page ------------------------------------------------------------

    Friend Function PageFile(helpId As String, lang As String) As String
        Dim root = Help.Root()
        If root = "" OrElse helpId = "" Then Return ""
        For Each l In New String() {lang, "en"}
            Dim f = Path.Combine(root, l, helpId & ".html")
            If File.Exists(f) Then Return f
        Next
        Return ""
    End Function

    Friend Function PageExists(helpId As String, lang As String) As Boolean
        Return PageFile(helpId, lang) <> ""
    End Function

    ' Where a target opens: the page beside the exe when it is there, the same page on the site when
    ' it is not (a bare exe copied elsewhere). The theme and the element ride in the address.
    Friend Function Address(target As HelpTarget) As String
        Dim lang = ShellSettings.Language()
        Dim themeName = If(Theme.Current.IsDark, "dark", "light")
        Dim found = PageFile(target.HelpId, lang)
        Dim tail = "?theme=" & themeName & If(target.Anchor <> "", "#" & target.Anchor, "")
        If found <> "" Then
            Dim readable = ReadableCopyOf(found)
            Return New Uri(readable).AbsoluteUri & tail
        End If
        Return Links.Site & "help/" & lang & "/" & target.HelpId & ".html" & tail
    End Function

    ' A Store build runs from a package folder that other programs (the browser) may not read, and
    ' that is read-only. The pages are copied once per build into the temp folder and opened there.
    ' An unpackaged install is read in place.
    Private Function ReadableCopyOf(source As String) As String
        If RootForTest IsNot Nothing OrElse Not Packaging.IsPackaged() Then Return source
        Try
            Dim root = Help.Root()
            Dim dest = Path.Combine(Path.GetTempPath(), "FileDO-help", LogReport.BuildStamp())
            Dim marker = Path.Combine(dest, "copied")
            If Not File.Exists(marker) Then
                CopyTree(root, dest)
                File.WriteAllText(marker, "")
            End If
            Return Path.Combine(dest, source.Substring(root.Length).TrimStart("\"c))
        Catch ex As Exception
            ShellLog.Write("help copy", ex)
            Return source
        End Try
    End Function

    Private Sub CopyTree(src As String, dest As String)
        Directory.CreateDirectory(dest)
        For Each f In Directory.GetFiles(src)
            File.Copy(f, Path.Combine(dest, Path.GetFileName(f)), True)
        Next
        For Each d In Directory.GetDirectories(src)
            CopyTree(d, Path.Combine(dest, Path.GetFileName(d)))
        Next
    End Sub

    ' ---- the ask (rules 1-3) ---------------------------------------------------

    ' One entry for the key and the button alike. The page opens in the browser and nothing in the
    ' product closes or waits (rule 7): help from a modal dialog leaves the dialog answerable.
    Friend Sub Open(owner As IWin32Window, surfaceId As String, Optional elementId As String = Nothing)
        Go(owner, Address(Resolve(surfaceId, elementId)))
    End Sub

    Friend Sub OpenIndex(owner As IWin32Window)
        Go(owner, Address(IndexTarget()))
    End Sub

    ' For SelfTest.vb: where an ask would open, caught instead of opened, so a test never starts a browser.
    Friend OpenerForTest As Action(Of String) = Nothing

    Private Sub Go(owner As IWin32Window, address As String)
        If OpenerForTest IsNot Nothing Then
            OpenerForTest(address)
        ElseIf address.StartsWith("file:", StringComparison.OrdinalIgnoreCase) AndAlso TryDefaultBrowser(address) Then
            Return
        Else
            Links.Open(owner, address)
        End If
    End Sub

    ' The shell's own "open" of a file address hands the default program the path alone and drops the
    ' query and the fragment - the theme and the element would be lost. So a page on this machine is
    ' opened by the default web browser's own command line, the way a link click would start it. When no
    ' browser can be found that way the address goes to Links.Open and the page still opens, in the system's
    ' theme and at its top.
    Private Function TryDefaultBrowser(address As String) As Boolean
        Try
            Dim command As String = Nothing
            For Each scheme In New String() {"https", "http"}
                Dim progId = TryCast(Microsoft.Win32.Registry.GetValue(
                    "HKEY_CURRENT_USER\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\" & scheme & "\UserChoice", "ProgId", Nothing), String)
                If String.IsNullOrEmpty(progId) Then Continue For
                command = TryCast(Microsoft.Win32.Registry.GetValue("HKEY_CLASSES_ROOT\" & progId & "\shell\open\command", "", Nothing), String)
                If Not String.IsNullOrEmpty(command) Then Exit For
            Next
            Dim exe As String = Nothing
            Dim args As String = Nothing
            If Not SplitBrowserCommand(command, address, exe, args) Then Return False
            Process.Start(New ProcessStartInfo(exe, args) With {.UseShellExecute = False})
            Return True
        Catch ex As Exception
            ShellLog.Write("help browser", ex)
            Return False
        End Try
    End Function

    ' A registered open command ("chrome.exe" --single-argument %1, firefox.exe -osint -url "%1") into the
    ' program and its arguments, the address put where the command says %1 (or %L), or after the rest when it
    ' names no place for it.
    Friend Function SplitBrowserCommand(command As String, address As String, ByRef exe As String, ByRef args As String) As Boolean
        If String.IsNullOrWhiteSpace(command) Then Return False
        Dim text = command.Trim()
        Dim rest As String
        If text.StartsWith("""", StringComparison.Ordinal) Then
            Dim close = text.IndexOf(""""c, 1)
            If close < 0 Then Return False
            exe = text.Substring(1, close - 1)
            rest = text.Substring(close + 1).Trim()
        Else
            Dim space = text.IndexOf(" "c)
            exe = If(space < 0, text, text.Substring(0, space))
            rest = If(space < 0, "", text.Substring(space + 1).Trim())
        End If
        exe = Environment.ExpandEnvironmentVariables(exe)
        If exe = "" OrElse Not exe.EndsWith(".exe", StringComparison.OrdinalIgnoreCase) Then Return False
        If rest.Contains("%1") OrElse rest.Contains("%L") OrElse rest.Contains("%l") Then
            args = rest.Replace("%1", address).Replace("%L", address).Replace("%l", address)
        Else
            args = (rest & " """ & address & """").Trim()
        End If
        Return True
    End Function

    ' The tooltip and accessible name of the button: the vocabulary name of app.help in the UI
    ' language, the key appended (rule 2).
    Friend Function ButtonName() As String
        Return Localization.T("vd_mgr_name_help")
    End Function

    Friend Function ButtonTip() As String
        Return ButtonName() & " (F1)"
    End Function

    ' The `?` button: the app.help glyph, icon only, named. The caller places it in the trailing end
    ' of the surface's first row and wires its click to Open.
    Friend Function NewButton() As GlyphButton
        Dim b As New GlyphButton With {
            .Glyph = GlyphRef.Vocabulary("app.help"),
            .Tier = 24,
            .IconOnly = True,
            .Name = "help:help",
            .Anchor = AnchorStyles.Top Or AnchorStyles.Right,
            .Margin = New Padding(12, 0, 0, 0)
        }
        b.AccessibleName = ButtonName()
        Return b
    End Function
End Module
