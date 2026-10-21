Imports System.IO
Imports System.Reflection
Imports System.Text.RegularExpressions

' The in-app help against the contract (SP-0168; CONTEXT-HELP rule 19 and rung 1 of section 4): the map
' against the surfaces the source defines, the pages against the languages, the key and the button, the
' anatomy of a page. What a person must still look at - the picture, the wording, the walk of rung 3 -
' is not here.
'
' Debt is a baseline that may only fall (CHECK-BASELINE): docs\help\help-baseline.txt holds one line per
' thing that is not done yet, `page <help id>` for a page not written and `ask <surface id>` for a surface
' whose key and button are not wired. A line whose debt is paid fails as stale, so the file shrinks.
Partial Public Module SelfTest

    ' The windows whose F1 and help button are wired (rule 2-3); a surface of any other class owes an
    ' `ask` line in the baseline until its class is added here.
    Private ReadOnly WiredHelpClasses As String() = {"ShellForm", "DiskManagerForm"}

    Private Function HelpBaseline() As List(Of String)
        Dim lines As New List(Of String)
        Dim root = Help.Root()
        If root = "" Then Return lines
        Dim f = Path.Combine(root, "help-baseline.txt")
        If Not File.Exists(f) Then Return lines
        For Each ln In File.ReadAllLines(f)
            Dim t = ln.Trim()
            If t <> "" AndAlso Not t.StartsWith("#", StringComparison.Ordinal) Then lines.Add(t)
        Next
        Return lines
    End Function

    Private Function Few(items As IEnumerable(Of String)) As String
        Return String.Join(", ", items.Take(8).ToArray())
    End Function

    Private Sub CheckHelp()
        Help.ResetForTest()
        Dim map = Help.Map()
        Check("help:map-loads", map IsNot Nothing AndAlso map.SchemaVersion = 1 AndAlso map.Surfaces.Count > 0, Help.Root())
        If map Is Nothing Then Return
        CheckHelpMapAgainstSource(map)
        CheckHelpContent()
        CheckHelpResolver(map)
        CheckHelpKeyAndButton()
        CheckHelpPages()
        CheckHelpStrings()
    End Sub

    ' ---- the map against the source (rule 19) ----------------------------------------

    Private Sub CheckHelpMapAgainstSource(map As HelpMap)
        Dim baseline = HelpBaseline()
        Dim en = Localization.GetDict("en")
        Dim railKeys = RailRow.All.Where(Function(r) Not r.IsGroup).Select(Function(r) r.Key).ToList()
        Dim classNames = GetType(ShellForm).Assembly.GetTypes().
            Where(Function(t) t.IsSubclassOf(GetType(Form)) AndAlso Not t.IsAbstract AndAlso Not t.Name.StartsWith("_", StringComparison.Ordinal)).
            Select(Function(t) t.Name).ToList()

        Check("help:rail-rows", railKeys.All(Function(k) map.ByRail(k) IsNot Nothing),
              Few(railKeys.Where(Function(k) map.ByRail(k) Is Nothing)))
        Check("help:class-rows", classNames.All(Function(c) map.Surfaces.Any(Function(s) s.ClassName = c)),
              Few(classNames.Where(Function(c) Not map.Surfaces.Any(Function(s) s.ClassName = c))))
        Check("help:no-ghost-rows",
              map.Surfaces.All(Function(s) (s.Rail = "" OrElse railKeys.Contains(s.Rail)) AndAlso (s.ClassName = "" OrElse classNames.Contains(s.ClassName))),
              Few(map.Surfaces.Where(Function(s) (s.Rail <> "" AndAlso Not railKeys.Contains(s.Rail)) OrElse (s.ClassName <> "" AndAlso Not classNames.Contains(s.ClassName))).Select(Function(s) s.Id)))
        Check("help:ids-unique", map.Surfaces.Select(Function(s) s.Id).Distinct().Count() = map.Surfaces.Count, "")

        Dim forms = New String() {"layer", "page", "both", "inherit", "none"}
        Check("help:forms", map.Surfaces.All(Function(s) forms.Contains(s.Form)), Few(map.Surfaces.Where(Function(s) Not forms.Contains(s.Form)).Select(Function(s) s.Id)))
        Check("help:reasons", map.Surfaces.Where(Function(s) s.Form = "inherit" OrElse s.Form = "none").All(Function(s) s.Reason.Trim() <> ""),
              Few(map.Surfaces.Where(Function(s) (s.Form = "inherit" OrElse s.Form = "none") AndAlso s.Reason.Trim() = "").Select(Function(s) s.Id)))
        Check("help:inherit-parents", map.Surfaces.Where(Function(s) s.Form = "inherit").All(Function(s) InheritsFromPage(map, s)),
              Few(map.Surfaces.Where(Function(s) s.Form = "inherit" AndAlso Not InheritsFromPage(map, s)).Select(Function(s) s.Id)))
        Check("help:names", map.Surfaces.All(Function(s) en.ContainsKey(s.NameKey) AndAlso en(s.NameKey) = s.Name),
              Few(map.Surfaces.Where(Function(s) Not (en.ContainsKey(s.NameKey) AndAlso en(s.NameKey) = s.Name)).Select(Function(s) s.Id)))
        Check("help:page-rows-have-ids", map.Surfaces.Where(Function(s) s.Form = "page" OrElse s.Form = "both").All(Function(s) s.HelpId <> ""), "")

        ' A page row names a page that exists or is baselined as not written yet.
        Dim pageRows = map.Surfaces.Where(Function(s) s.HelpId <> "").Select(Function(s) s.HelpId).Distinct().ToList()
        Dim unwritten = pageRows.Where(Function(h) HelpContent.PageById(h) Is Nothing).ToList()
        Dim owedPages = baseline.Where(Function(b) b.StartsWith("page ", StringComparison.Ordinal)).Select(Function(b) b.Substring(5)).ToList()
        Check("help:pages-written-or-baselined", unwritten.All(Function(h) owedPages.Contains(h)), Few(unwritten.Where(Function(h) Not owedPages.Contains(h))))
        Check("help:baseline-pages-not-stale", owedPages.All(Function(h) unwritten.Contains(h)), Few(owedPages.Where(Function(h) Not unwritten.Contains(h))))
        Check("help:fallbacks-written", map.Surfaces.Where(Function(s) s.Fallback <> "").All(Function(s) HelpContent.PageById(s.Fallback) IsNot Nothing), "")

        ' A surface that has help has its key and button wired, or is baselined as not yet.
        Dim unwired = map.Surfaces.Where(Function(s) s.Form <> "none" AndAlso s.Rail = "" AndAlso Not WiredHelpClasses.Contains(s.ClassName)).
                      Select(Function(s) s.Id).ToList()
        Dim owedAsk = baseline.Where(Function(b) b.StartsWith("ask ", StringComparison.Ordinal)).Select(Function(b) b.Substring(4)).ToList()
        Check("help:wired-or-baselined", unwired.All(Function(i) owedAsk.Contains(i)), Few(unwired.Where(Function(i) Not owedAsk.Contains(i))))
        Check("help:baseline-ask-not-stale", owedAsk.All(Function(i) unwired.Contains(i)), Few(owedAsk.Where(Function(i) Not unwired.Contains(i))))
        Check("help:baseline-known-lines", baseline.All(Function(b) b.StartsWith("page ", StringComparison.Ordinal) OrElse b.StartsWith("ask ", StringComparison.Ordinal)),
              Few(baseline.Where(Function(b) Not (b.StartsWith("page ", StringComparison.Ordinal) OrElse b.StartsWith("ask ", StringComparison.Ordinal)))))
    End Sub

    ' An inheriting surface climbs to a parent that has a page of its own, without a loop.
    Private Function InheritsFromPage(map As HelpMap, s As HelpSurface) As Boolean
        Dim cur = s
        For i = 1 To 8
            If cur.Form <> "inherit" Then Return cur.Form = "page" OrElse cur.Form = "both" OrElse cur.Form = "layer"
            cur = map.ById(cur.Parent)
            If cur Is Nothing Then Return False
        Next
        Return False
    End Function

    ' ---- the content table ----------------------------------------------------------------

    Private Sub CheckHelpContent()
        Dim pages = HelpContent.Pages
        Check("help:pages-unique", pages.Select(Function(p) p.Id).Distinct().Count() = pages.Length, "")
        For Each p In pages
            Dim ids = p.Elements.Select(Function(e) e.Id).ToList()
            Check("help:content:" & p.Id & ":elements", ids.Distinct().Count() = ids.Count AndAlso ids.Contains("help"), "")
            Check("help:content:" & p.Id & ":steps", p.StepKeys.Length <= 7, p.StepKeys.Length.ToString())
            Dim en = Localization.GetDict("en")
            Dim badRefs As New List(Of String)
            For Each k In p.StepKeys
                For Each m As Match In Regex.Matches(en(k), "\[([a-z0-9-]+)\]")
                    If Not ids.Contains(m.Groups(1).Value) Then badRefs.Add(k & ":" & m.Groups(1).Value)
                Next
            Next
            Check("help:content:" & p.Id & ":step-references", badRefs.Count = 0, Few(badRefs))
            Dim locators = p.Elements.Where(Function(e) e.Locator <> "").Select(Function(e) e.Locator).ToList()
            Check("help:content:" & p.Id & ":locators", locators.Distinct().Count() = locators.Count, "")
        Next

        ' Every key a page reads is in every language: the key sets of the help tables are equal (the
        ' general locale-keys rows check the merged tables too), and every other key exists in English.
        For Each lang In Localization.Languages
            Dim own = Localization.OwnKeysForTest(lang)
            Dim missing As New List(Of String)
            Dim dict = Localization.GetDict(lang)
            For Each k In HelpKeys()
                If Not dict.ContainsKey(k) Then missing.Add(k)
                If k.StartsWith("help_", StringComparison.Ordinal) AndAlso Not own.Contains(k) Then missing.Add(k & " (own)")
            Next
            Check("help:keys:" & lang, missing.Count = 0, Few(missing))
        Next
    End Sub

    Private Function HelpKeys() As List(Of String)
        Dim keys As New List(Of String)
        For Each p In HelpContent.Pages
            keys.Add(p.TitleKey)
            keys.Add(p.ForKey)
            keys.AddRange(p.StepKeys)
            If p.NoOrderKey <> "" Then keys.Add(p.NoOrderKey)
            For Each e In p.Elements
                keys.Add(e.NameKey)
                keys.Add(e.OneKey)
                keys.Add(e.FullKey)
                If e.NextKey <> "" Then keys.Add(e.NextKey)
                If e.UndoKey <> "" Then keys.Add(e.UndoKey)
            Next
            For Each r In p.Related
                keys.Add(r.TitleKey)
                keys.Add(r.NoteKey)
            Next
        Next
        For Each k In New String() {"help_ui_index", "help_ui_kind_window", "help_ui_kind_index", "help_ui_order", "help_ui_elements", "help_ui_related",
                                    "help_ui_next", "help_ui_undo", "help_ui_shortcut", "help_ui_demo", "help_ui_unpinned", "help_ui_pill_window",
                                    "help_ui_pill_site", "help_ui_pill_sibling", "help_ui_pill_portal", "help_ui_captured", "help_ui_support", "help_ui_alt",
                                    "help_ix_title", "help_ix_lead", "help_ix_main", "help_ix_disks", "help_ix_site", "help_ix_dm_note",
                                    "help_ix_guides_title", "help_ix_guides_note", "tray_help"}
            keys.Add(k)
        Next
        Return keys.Distinct().ToList()
    End Function

    ' ---- the resolver (rule 17) -----------------------------------------------------------

    Private Sub CheckHelpResolver(map As HelpMap)
        Dim command = Help.Resolve("shell.command")
        Check("help:resolve:page", command.HelpId = "command" AndAlso command.Anchor = "", command.HelpId)
        Check("help:resolve:element", Help.Resolve("shell.command", "operation").Anchor = "e-operation", "")
        Check("help:resolve:unknown-element", Help.Resolve("shell.command", "nothing-here").Anchor = "", "")
        Check("help:resolve:settings-row", Help.Resolve("shell.settings", "setting-theme").Anchor = "e-setting-theme", "")
        Dim wipe = Help.Resolve("shell.wipe")
        Check("help:resolve:fallback", wipe.HelpId = If(Help.PageExists("job-wipe", "en"), "job-wipe", "job-page"), wipe.HelpId)
        Check("help:resolve:frame-element", Help.Resolve("shell.damaged", "target").Anchor = "e-target" OrElse Help.PageExists("job-damaged", "en"), "")
        Check("help:resolve:inherit", Help.Resolve("disk.password").HelpId = "disk-manager" AndAlso Help.Resolve("disk.settings").HelpId = "settings", "")
        Check("help:resolve:none", Help.Resolve("shell-dialog").HelpId = Help.IndexId AndAlso Help.Resolve("disk.base").HelpId = Help.IndexId, "")
        Check("help:resolve:unknown-surface", Help.Resolve("no-such-surface").HelpId = Help.IndexId, "")
        Check("help:resolve:disk-manager", Help.Resolve("disk-manager", "mount").Anchor = "e-mount", "")

        ' The address: the theme rides along, the element is the fragment, the page is a file or the site's.
        Dim addr = Help.Address(Help.Resolve("shell.settings", "setting-theme"))
        Check("help:address", addr.Contains("settings.html") AndAlso addr.Contains("?theme=") AndAlso addr.EndsWith("#e-setting-theme", StringComparison.Ordinal), addr)

        ' A page is chosen by the application's language and falls back to English, never to nothing.
        Dim temp = Path.Combine(Path.GetTempPath(), "filedo_help_test_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(Path.Combine(temp, "en"))
            Directory.CreateDirectory(Path.Combine(temp, "de"))
            File.WriteAllText(Path.Combine(temp, "en", "p.html"), "x")
            File.WriteAllText(Path.Combine(temp, "de", "q.html"), "x")
            File.WriteAllText(Path.Combine(temp, "en", "q.html"), "x")
            Help.RootForTest = temp
            Check("help:file:own-language", Help.PageFile("q", "de").EndsWith("de\q.html", StringComparison.Ordinal), "")
            Check("help:file:english-fallback", Help.PageFile("p", "de").EndsWith("en\p.html", StringComparison.Ordinal), "")
            Check("help:file:missing", Help.PageFile("zz", "de") = "", "")
        Finally
            Help.RootForTest = Nothing
            Try
                Directory.Delete(temp, True)
            Catch
            End Try
        End Try

        ' The browser's registered command line: the address goes where the command says %1, the program is
        ' what is in front of it (the shell's own open of a file address would drop the query and the anchor).
        Dim exe As String = Nothing
        Dim args As String = Nothing
        Dim url = "file:///C:/Program%20Files/FileDO/help/en/settings.html?theme=dark#e-setting-theme"
        Check("help:browser:chrome-style", Help.SplitBrowserCommand("""C:\Program Files\Google\Chrome\Application\chrome.exe"" --single-argument %1", url, exe, args) AndAlso
              exe = "C:\Program Files\Google\Chrome\Application\chrome.exe" AndAlso args = "--single-argument " & url, exe & " | " & args)
        Check("help:browser:firefox-style", Help.SplitBrowserCommand("""C:\Program Files\Mozilla Firefox\firefox.exe"" -osint -url ""%1""", url, exe, args) AndAlso
              args = "-osint -url """ & url & """", args)
        Check("help:browser:no-place-for-it", Help.SplitBrowserCommand("""C:\Apps\b.exe"" --new-window", url, exe, args) AndAlso args = "--new-window """ & url & """", args)
        Check("help:browser:not-a-program", Not Help.SplitBrowserCommand("", url, exe, args) AndAlso Not Help.SplitBrowserCommand("rundll32.dll x %1", url, exe, args), "")

        ' F1 finds the part a focus is in from the names the views give.
        Using host As New Panel With {.Name = "help:target"}, field As New TextBox(), row As New Panel With {.Name = "setting:theme"}, inner As New TextBox()
            host.Controls.Add(field)
            row.Controls.Add(inner)
            Check("help:element-of:named-parent", Help.ElementOf(field) = "target", Help.ElementOf(field))
            Check("help:element-of:setting-row", Help.ElementOf(inner) = "setting-theme", Help.ElementOf(inner))
            Check("help:element-of:nothing", Help.ElementOf(New Label()) = "", "")
        End Using
    End Sub

    ' ---- the key and the button (rules 1-3) ------------------------------------------------

    Private Sub Click(c As Control)
        GetType(Control).GetMethod("OnClick", BindingFlags.Instance Or BindingFlags.NonPublic).Invoke(c, New Object() {EventArgs.Empty})
    End Sub

    Private Sub CheckHelpKeyAndButton()
        Dim opened As New List(Of String)
        Help.OpenerForTest = Sub(a) opened.Add(a)
        Try
            Using shell As New ShellForm()
                Dim button = shell.HelpButtonForTest
                Dim row = shell.HeaderRowControlsForTest
                ' The button is the only addition to the corner row (a control-set diff against the
                ' title stack that was alone in it before), and it is the trailing one.
                Check("help:button:only-addition", row.Count = 2 AndAlso row.Contains(button) AndAlso row(row.Count - 1) Is button, row.Count.ToString())
                Check("help:button:named", button.AccessibleName = Localization.T("vd_mgr_name_help") AndAlso button.IconOnly, button.AccessibleName)
                opened.Clear()
                Click(button)
                Dim byButton = If(opened.Count = 1, opened(0), "")
                opened.Clear()
                Dim handled = shell.HandleKeyForTest(Keys.F1)
                Dim byKey = If(opened.Count = 1, opened(0), "")
                Check("help:key-and-button-agree", handled AndAlso byButton <> "" AndAlso byButton = byKey, byButton & " | " & byKey)
                Dim expected = Help.Address(Help.Resolve(Help.ShellSurfaceId(shell.CurrentRailKey())))
                Check("help:key-opens-the-page-on-screen", byKey = expected, byKey)
            End Using
            DiskManagerForm.SuppressWelcome = True
            DiskManagerForm.SuppressReads = True
            Using m As New DiskManagerForm()
                opened.Clear()
                Click(m.HelpButtonForTest)
                Dim byButton = If(opened.Count = 1, opened(0), "")
                opened.Clear()
                Dim ranKey = m.RunShortcut(DiskShortcuts.Find(Keys.F1, False))
                Dim byKey = If(opened.Count = 1, opened(0), "")
                Check("help:disk-manager:key-and-button-agree", ranKey AndAlso byButton <> "" AndAlso byButton = byKey, byButton & " | " & byKey)
                Check("help:disk-manager:the-page", byKey = Help.Address(Help.Resolve("disk-manager")), byKey)
                Check("help:disk-manager:button-named", m.HelpButtonForTest.AccessibleName = Localization.T("vd_mgr_name_help"), "")
            End Using
        Finally
            Help.OpenerForTest = Nothing
        End Try
        Check("help:tray-entry", TrayIcon.MenuKeys(False, 0, 0).Contains("tray_help") AndAlso TrayIcon.MenuKeys(True, 0, 0).Contains("tray_help"), "")
    End Sub

    ' ---- the pages (rules 8-14) -------------------------------------------------------------

    Private Sub CheckHelpPages()
        Dim shot As New HelpShot With {.Width = 1180, .Height = 760}
        Check("help:languages", HelpPages.HelpLanguages.SequenceEqual(Localization.Languages), "")
        For Each lang In Localization.Languages
            Dim dict = Localization.GetDict(lang)
            For Each p In HelpContent.Pages
                shot.Pins.Clear()
                For Each e In p.Elements.Where(Function(x) x.Locator <> "" AndAlso x.Extra = "")
                    shot.Pins.Add(New HelpPin With {.ElementId = e.Id, .X = 10, .Y = 10})
                Next
                Dim html = HelpPages.RenderPage(p, lang, dict, shot, "2610211200")
                Dim problems = HelpPageProblems(p, lang, dict, html)
                Check("help:page:" & p.Id & ":" & lang, problems.Count = 0, Few(problems))
            Next
            Dim index = HelpPages.RenderIndex(lang, dict, "2610211200")
            Dim missingLinks = HelpContent.Pages.Where(Function(p) Not index.Contains("href=""" & p.Id & ".html""")).Select(Function(p) p.Id)
            Check("help:index:" & lang, index.Contains("lang=""" & lang & """") AndAlso Not missingLinks.Any(), Few(missingLinks))
        Next
    End Sub

    ' What a page must be (rules 8, 10, 12, 13), read off the text a page is written as.
    Private Function HelpPageProblems(p As HelpPageDef, lang As String, dict As Dictionary(Of String, String), whole As String) As List(Of String)
        Dim bad As New List(Of String)
        ' The two scripts are progressive enhancement and carry selectors that look like links.
        Dim html = Regex.Replace(whole, "<script>.*?</script>", "", RegexOptions.Singleline)
        If Not html.Contains("<html lang=""" & lang & """ dir=""ltr"">") Then bad.Add("lang/dir")
        If Not html.Contains("<meta name=""referrer"" content=""no-referrer"">") Then bad.Add("referrer")
        ' The eight parts, in order.
        Dim marks = New String() {"<h1>", "class=""lead""", "class=""shot""", "<h2>", "class=""elements""", "class=""related""", "class=""help-foot"""}
        Dim at = -1
        For Each mk In marks
            Dim i = html.IndexOf(mk, at + 1, StringComparison.Ordinal)
            If i < 0 Then
                bad.Add("part " & mk)
                Exit For
            End If
            at = i
        Next
        If Not Regex.IsMatch(html, "<img class=""on-dark""[^>]* alt=""[^""]+""") Then bad.Add("alt")
        ' No request of its own: nothing but an anchor leads to another origin, and no style imports one.
        If Regex.IsMatch(html, "<(?!a )[^>]*\b(src|href)=""https?:") Then bad.Add("remote resource")
        If html.Contains("@import") OrElse html.Contains("url(") OrElse Regex.IsMatch(html, "<script[^>]*\bsrc=") Then bad.Add("import/url/script src")
        If Regex.IsMatch(html, "<link[^>]*(fonts\.googleapis|googleapis)") Then bad.Add("web font")
        ' Entries.
        Dim ids = Regex.Matches(html, "<details class=""el"" id=""([^""]+)""").Cast(Of Match)().Select(Function(m) m.Groups(1).Value).ToList()
        If ids.Count <> p.Elements.Length OrElse ids.Distinct().Count() <> ids.Count Then bad.Add("entries " & ids.Count.ToString())
        For Each m As Match In Regex.Matches(html, "href=""#([^""]+)""")
            If Not ids.Contains(m.Groups(1).Value) Then bad.Add("anchor " & m.Groups(1).Value)
        Next
        For Each m As Match In Regex.Matches(html, "href=""([a-z0-9-]+)\.html""")
            Dim target = m.Groups(1).Value
            If target <> Help.IndexId AndAlso HelpContent.PageById(target) Is Nothing Then bad.Add("link " & target)
        Next
        ' The title is the surface's name as the UI shows it.
        If Not html.Contains("<h1>" & HelpPages.HtmlText(dict(p.TitleKey)) & "</h1>") Then bad.Add("title")
        If html.Contains("...") Then bad.Add("three dots")
        Return bad
    End Function

    ' ---- the words -------------------------------------------------------------------------

    ' The house style of the corpus applies to the help text too: ".." never "...", a plain hyphen, no
    ' "|" inside a value (GetDict splits on the first one).
    Private Sub CheckHelpStrings()
        For Each pair In New KeyValuePair(Of String, String())() {
                New KeyValuePair(Of String, String())("en", HelpStrings.EnLines()),
                New KeyValuePair(Of String, String())("ru", HelpStrings.RuLines()),
                New KeyValuePair(Of String, String())("uk", HelpStrings.UkLines()),
                New KeyValuePair(Of String, String())("de", HelpStrings.DeLines()),
                New KeyValuePair(Of String, String())("fr", HelpStrings.FrLines())}
            Dim bad As New List(Of String)
            Dim seen As New HashSet(Of String)
            For Each ln In pair.Value
                Dim i = ln.IndexOf("|"c)
                If i <= 0 Then
                    bad.Add("no key: " & ln.Substring(0, Math.Min(20, ln.Length)))
                    Continue For
                End If
                Dim key = ln.Substring(0, i)
                Dim value = ln.Substring(i + 1)
                If Not seen.Add(key) Then bad.Add("twice " & key)
                If value.Contains("|") OrElse value.Contains("...") OrElse value.Contains(ChrW(&H2014)) OrElse value.Contains(ChrW(&H2013)) Then bad.Add("style " & key)
                If value.Trim() = "" Then bad.Add("empty " & key)
            Next
            Check("help:strings:style:" & pair.Key, bad.Count = 0, Few(bad))
        Next
        ' One set of keys in every language.
        Dim reference = HelpStrings.EnLines().Select(Function(l) l.Substring(0, l.IndexOf("|"c))).ToList()
        For Each pair In New KeyValuePair(Of String, String())() {
                New KeyValuePair(Of String, String())("ru", HelpStrings.RuLines()),
                New KeyValuePair(Of String, String())("uk", HelpStrings.UkLines()),
                New KeyValuePair(Of String, String())("de", HelpStrings.DeLines()),
                New KeyValuePair(Of String, String())("fr", HelpStrings.FrLines())}
            Dim mine = pair.Value.Select(Function(l) l.Substring(0, l.IndexOf("|"c))).ToList()
            Check("help:strings:parity:" & pair.Key, mine.OrderBy(Function(k) k).SequenceEqual(reference.OrderBy(Function(k) k)),
                  Few(reference.Except(mine).Concat(mine.Except(reference))))
        Next
    End Sub
End Module
