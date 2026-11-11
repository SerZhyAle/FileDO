Imports System.Globalization
Imports System.IO
Imports System.Net
Imports System.Text

' The help pages, written (SP-0168; contract CONTEXT-HELP 0.1 rules 8-14).
'
' `filedo_win.exe --build-help <folder>` renders every page of HelpContent in every shipped language
' from the build itself: the words from the one string source, the picture and the position of each
' numbered marker from the real window (HelpCapture.vb). Nothing in a page is typed by hand, and the
' output is a render target - it is regenerated, never edited (canon invariant 16).
'
' A page is one anatomy, in this order (rule 8): title, what it is for, the picture with numbered
' markers, the path in order, the elements, related pages, and a footer. It makes no request (rule 12):
' no font, no remote script or image, the referrer is withheld, and it reads completely with the script
' off. The only script is progressive: it follows the application's theme from the address and opens the
' entry a marker points at. A page is in one language (rule 13), set in `lang` and `dir`.
Friend Class HelpPin
    Friend ElementId As String = ""
    ' Percent of the picture, from its top-left corner.
    Friend X As Double
    Friend Y As Double
End Class

Friend Class HelpShot
    Friend Width As Integer
    Friend Height As Integer
    Friend ReadOnly Pins As New List(Of HelpPin)
    ' The elements this surface does not show in the language of the picture (CONTEXT-HELP 0.2 section 7 item C).
    ' They are never pinned; the page marks each one, and check-help.ps1 counts them against the baseline.
    Friend ReadOnly Absent As New List(Of String)

    Friend Function PinFor(elementId As String) As HelpPin
        Return Pins.FirstOrDefault(Function(p) p.ElementId = elementId)
    End Function
End Class

Friend Module HelpPages
    ' The languages the pages are written in and the folder each one lives in.
    Friend ReadOnly HelpLanguages As String() = Localization.Languages
    Friend ReadOnly HelpThemes As String() = {"dark", "light"}

    ' ---- the whole set -----------------------------------------------------------

    ' 0 when every file was written, 1 otherwise. Each written path goes to stdout.
    Friend Function Build(folder As String) As Integer
        Dim failed = False
        Dim saved = ShellSettings.ValuesForTest
        ' Nothing is read from or written to the user's settings: the capture builds real windows, and
        ' a window writes its placement and its collapsed groups when it goes.
        ShellSettings.ValuesForTest = New Dictionary(Of String, Object)()
        Try
            Directory.CreateDirectory(folder)
            Directory.CreateDirectory(Path.Combine(folder, "img"))
            CopyKit(folder)
            Dim stamp = LogReport.BuildStamp()
            For Each lang In HelpLanguages
                ShellSettings.LanguageOverride = lang
                Localization.ResetShellDict()
                Dim dict = Localization.GetDict(lang)
                Directory.CreateDirectory(Path.Combine(folder, lang))
                For Each page In HelpContent.Pages
                    Dim shots As New Dictionary(Of String, HelpShot)
                    For Each themeName In HelpThemes
                        Try
                            Dim png = Path.Combine(folder, "img", page.Id & "." & lang & "." & themeName & ".png")
                            shots(themeName) = HelpCapture.Take(page, themeName = "dark", png)
                            Console.WriteLine(png)
                        Catch ex As Exception
                            failed = True
                            ShellLog.Write("help capture " & page.Id & " " & lang & " " & themeName, ex)
                            Console.Error.WriteLine("capture failed: " & page.Id & " " & lang & " " & themeName & " (" & ex.GetType().Name & ", see the shell log)")
                        End Try
                    Next
                    If shots.ContainsKey("dark") Then
                        Dim pageFile = Path.Combine(folder, lang, page.Id & ".html")
                        WriteText(pageFile, RenderPage(page, lang, dict, shots("dark"), stamp))
                        Console.WriteLine(pageFile)
                    End If
                Next
                Dim indexFile = Path.Combine(folder, lang, Help.IndexId & ".html")
                WriteText(indexFile, RenderIndex(lang, dict, stamp))
                Console.WriteLine(indexFile)
            Next
        Catch ex As Exception
            failed = True
            ShellLog.Write("build-help", ex)
            Console.Error.WriteLine("build-help failed (" & ex.GetType().Name & ", see the shell log)")
        Finally
            ShellSettings.LanguageOverride = Nothing
            Localization.ResetShellDict()
            Theme.UsePaletteForTest(Nothing)
            ShellSettings.ValuesForTest = saved
        End Try
        Return If(failed, 1, 0)
    End Function

    ' The page set carries the site's kit beside it, so a page opened from the package and the same page
    ' on the site draw the same (rule 12: no font or sheet from outside).
    Private Sub CopyKit(folder As String)
        Dim kit = Path.GetFullPath(Path.Combine(folder, "..", "kit", "sza-kit.css"))
        If File.Exists(kit) Then File.Copy(kit, Path.Combine(folder, "sza-kit.css"), True)
    End Sub

    ' LF line ends and no byte order mark, the way every other page of the site is written.
    Private Sub WriteText(target As String, text As String)
        File.WriteAllText(target, text.Replace(vbCrLf, vbLf), New UTF8Encoding(False))
    End Sub

    ' ---- one page ------------------------------------------------------------------

    ' Markup-safe text. Only the characters markup uses are escaped: the page is UTF-8, so "ä" and "ё"
    ' stay what they are, and a person who reads the source reads the words.
    Friend Function HtmlText(text As String) As String
        If text Is Nothing Then Return ""
        Return text.Replace("&", "&amp;").Replace("<", "&lt;").Replace(">", "&gt;").Replace("""", "&quot;")
    End Function

    Private Function Pct(v As Double) As String
        Return v.ToString("0.0", CultureInfo.InvariantCulture) & "%"
    End Function

    Private Function T(dict As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If key <> "" AndAlso dict.TryGetValue(key, v) Then Return Localization.Multiline(v)
        Return key
    End Function

    ' The name of a part as the UI spells it, without the colon a label carries before its box.
    Private Function PartName(dict As Dictionary(Of String, String), e As HelpElementDef) As String
        Return T(dict, e.NameKey).Trim().TrimEnd(":"c).Trim()
    End Function

    Private Function Kbd(chord As String) As String
        Dim parts = chord.Split("+"c)
        Return String.Join(" + ", parts.Select(Function(p) "<kbd>" & HtmlText(p.Trim()) & "</kbd>"))
    End Function

    Friend Function VersionOf(stamp As String) As String
        Return stamp
    End Function

    ' The date a stamped build was made (yyMMddHHmm), or today for a build that has no stamp. A page
    ' regenerated from the same build is then the same bytes.
    Friend Function DateOf(stamp As String) As String
        Dim d As DateTime
        If stamp IsNot Nothing AndAlso stamp.Length = 10 AndAlso
           DateTime.TryParseExact(stamp, "yyMMddHHmm", CultureInfo.InvariantCulture, DateTimeStyles.None, d) Then
            Return d.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture)
        End If
        Return DateTime.Now.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture)
    End Function

    Private Const ThemeScript As String =
        "<script>" & vbLf &
        "(function () {" & vbLf &
        "  try {" & vbLf &
        "    var q = new URLSearchParams(location.search).get('theme');" & vbLf &
        "    var t = (q === 'light' || q === 'dark') ? q : (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');" & vbLf &
        "    document.documentElement.setAttribute('data-theme', t);" & vbLf &
        "  } catch (e) { /* no script: help.css follows the system */ }" & vbLf &
        "})();" & vbLf &
        "</script>"

    Private Const EnhanceScript As String =
        "<script>" & vbLf &
        "/* Progressive enhancement only: the page is complete without it. */" & vbLf &
        "(function () {" & vbLf &
        "  function openHash() {" & vbLf &
        "    var id = location.hash.slice(1);" & vbLf &
        "    var el = id && document.getElementById(id);" & vbLf &
        "    if (el && el.tagName === 'DETAILS') { el.open = true; }" & vbLf &
        "  }" & vbLf &
        "  addEventListener('hashchange', openHash);" & vbLf &
        "  openHash();" & vbLf &
        "  document.querySelectorAll('details.el').forEach(function (d) {" & vbLf &
        "    var pin = document.querySelector('.pin[href=""#' + d.id + '""]');" & vbLf &
        "    if (!pin) { return; }" & vbLf &
        "    function on() { pin.classList.add('on'); d.classList.add('on'); }" & vbLf &
        "    function off() { pin.classList.remove('on'); d.classList.remove('on'); }" & vbLf &
        "    d.addEventListener('mouseenter', on); d.addEventListener('mouseleave', off);" & vbLf &
        "    d.addEventListener('focusin', on); d.addEventListener('focusout', off);" & vbLf &
        "    pin.addEventListener('mouseenter', on); pin.addEventListener('mouseleave', off);" & vbLf &
        "    pin.addEventListener('focus', on); pin.addEventListener('blur', off);" & vbLf &
        "  });" & vbLf &
        "})();" & vbLf &
        "</script>"

    Private Function Head(lang As String, title As String, dict As Dictionary(Of String, String)) As String
        Dim sb As New StringBuilder()
        sb.Append("<!doctype html>").Append(vbLf)
        sb.Append("<html lang=""").Append(lang).Append(""" dir=""").Append(If(Localization.IsRightToLeft(lang), "rtl", "ltr")).Append(""">").Append(vbLf)
        sb.Append("<head>").Append(vbLf)
        sb.Append("<meta charset=""utf-8"">").Append(vbLf)
        sb.Append("<meta name=""viewport"" content=""width=device-width, initial-scale=1"">").Append(vbLf)
        sb.Append("<meta name=""referrer"" content=""no-referrer"">").Append(vbLf)
        sb.Append("<meta name=""robots"" content=""noindex"">").Append(vbLf)
        sb.Append("<title>").Append(HtmlText(T(dict, "help_ui_kind_index") & " - " & title)).Append("</title>").Append(vbLf)
        sb.Append(ThemeScript).Append(vbLf)
        sb.Append("<link rel=""stylesheet"" href=""../sza-kit.css"">").Append(vbLf)
        sb.Append("<link rel=""stylesheet"" href=""../help.css"">").Append(vbLf)
        sb.Append("</head>").Append(vbLf)
        Return sb.ToString()
    End Function

    Private Function Footer(dict As Dictionary(Of String, String), stamp As String) As String
        Dim sb As New StringBuilder()
        sb.Append("  <footer class=""help-foot"">").Append(vbLf)
        sb.Append("    <span>").Append(HtmlText(Localization.Format(T(dict, "help_ui_captured"), VersionOf(stamp), DateOf(stamp)))).Append("</span>").Append(vbLf)
        sb.Append("    <a href=""index.html"">").Append(HtmlText(T(dict, "help_ui_index"))).Append("</a>").Append(vbLf)
        sb.Append("    <a href=""").Append(HtmlText(Links.Site & "guides/support.html")).Append(""">").Append(HtmlText(T(dict, "help_ui_support"))).Append("</a>").Append(vbLf)
        sb.Append("  </footer>").Append(vbLf)
        Return sb.ToString()
    End Function

    ' A step names a part as [element-id]; it becomes the part's number, linked to its entry.
    Private Function StepHtml(text As String, page As HelpPageDef) As String
        Dim sb As New StringBuilder()
        Dim i = 0
        While i < text.Length
            Dim open = text.IndexOf("["c, i)
            If open < 0 Then
                sb.Append(HtmlText(text.Substring(i)))
                Exit While
            End If
            Dim close = text.IndexOf("]"c, open)
            If close < 0 Then
                sb.Append(HtmlText(text.Substring(i)))
                Exit While
            End If
            sb.Append(HtmlText(text.Substring(i, open - i)))
            Dim id = text.Substring(open + 1, close - open - 1)
            Dim n = Array.FindIndex(page.Elements, Function(e) e.Id = id) + 1
            If n > 0 Then
                sb.Append("<a class=""ref"" href=""#").Append(HelpContent.AnchorOf(id)).Append(""">").Append(n.ToString()).Append("</a>")
            End If
            i = close + 1
        End While
        Return sb.ToString()
    End Function

    Friend Function RenderPage(page As HelpPageDef, lang As String, dict As Dictionary(Of String, String),
                               shot As HelpShot, stamp As String) As String
        Dim title = T(dict, page.TitleKey)
        Dim sb As New StringBuilder()
        sb.Append(Head(lang, title, dict))
        sb.Append("<body class=""help"">").Append(vbLf)
        sb.Append("<main class=""container"">").Append(vbLf).Append(vbLf)

        ' 1. title
        sb.Append("  <nav class=""help-crumbs"" aria-label=""Breadcrumb"">").Append(vbLf)
        sb.Append("    <a href=""index.html"">").Append(HtmlText(T(dict, "help_ui_index"))).Append("</a><span class=""sep"" aria-hidden=""true"">/</span><span>").Append(HtmlText(title)).Append("</span>").Append(vbLf)
        sb.Append("  </nav>").Append(vbLf)
        sb.Append("  <header class=""hero"">").Append(vbLf)
        sb.Append("    <p class=""kicker"">").Append(HtmlText(T(dict, "help_ui_kind_window"))).Append("</p>").Append(vbLf)
        sb.Append("    <h1>").Append(HtmlText(title)).Append("</h1>").Append(vbLf)
        ' 2. what this is for
        sb.Append("    <p class=""lead"">").Append(HtmlText(T(dict, page.ForKey))).Append("</p>").Append(vbLf)
        sb.Append("  </header>").Append(vbLf).Append(vbLf)

        ' 3. the picture with numbered markers
        sb.Append("  <figure class=""shot"">").Append(vbLf)
        sb.Append("    <div class=""frame"">").Append(vbLf)
        Dim size = " width=""" & shot.Width.ToString() & """ height=""" & shot.Height.ToString() & """"
        Dim alt = Localization.Format(T(dict, "help_ui_alt"), title)
        sb.Append("      <img class=""on-dark"" src=""../img/").Append(page.Id).Append(".").Append(lang).Append(".dark.png""").Append(size).Append(" alt=""").Append(HtmlText(alt)).Append(""">").Append(vbLf)
        sb.Append("      <img class=""on-light"" src=""../img/").Append(page.Id).Append(".").Append(lang).Append(".light.png""").Append(size).Append(" alt="""">").Append(vbLf)
        For i = 0 To page.Elements.Length - 1
            Dim e = page.Elements(i)
            Dim pin = shot.PinFor(e.Id)
            If pin Is Nothing Then Continue For
            sb.Append("      <a class=""pin"" style=""--x:").Append(Pct(pin.X)).Append(";--y:").Append(Pct(pin.Y)).Append(""" href=""#").Append(HelpContent.AnchorOf(e.Id)).Append(""" aria-label=""")
            sb.Append((i + 1).ToString()).Append(", ").Append(HtmlText(PartName(dict, e))).Append(""">").Append((i + 1).ToString()).Append("</a>").Append(vbLf)
        Next
        sb.Append("    </div>").Append(vbLf)
        sb.Append("    <figcaption>").Append(HtmlText(T(dict, "help_ui_demo"))).Append("</figcaption>").Append(vbLf)
        sb.Append("  </figure>").Append(vbLf).Append(vbLf)

        ' 4. in this order
        sb.Append("  <h2>").Append(HtmlText(T(dict, "help_ui_order"))).Append("</h2>").Append(vbLf)
        If page.StepKeys.Length = 0 Then
            sb.Append("  <p>").Append(HtmlText(T(dict, page.NoOrderKey))).Append("</p>").Append(vbLf)
        Else
            sb.Append("  <ol class=""order"">").Append(vbLf)
            For Each k In page.StepKeys
                sb.Append("    <li>").Append(StepHtml(T(dict, k), page)).Append("</li>").Append(vbLf)
            Next
            sb.Append("  </ol>").Append(vbLf)
        End If
        sb.Append(vbLf)

        ' 5. the elements
        sb.Append("  <h2>").Append(HtmlText(T(dict, "help_ui_elements"))).Append("</h2>").Append(vbLf)
        sb.Append("  <div class=""elements"">").Append(vbLf)
        For i = 0 To page.Elements.Length - 1
            sb.Append(ElementHtml(page.Elements(i), i + 1, dict, shot.PinFor(page.Elements(i).Id) IsNot Nothing, shot.Absent.Contains(page.Elements(i).Id)))
        Next
        sb.Append("  </div>").Append(vbLf).Append(vbLf)

        ' 6. related
        sb.Append("  <h2>").Append(HtmlText(T(dict, "help_ui_related"))).Append("</h2>").Append(vbLf)
        sb.Append("  <ul class=""related"">").Append(vbLf)
        For Each r In page.Related
            sb.Append(RelatedHtml(r, dict))
        Next
        sb.Append("  </ul>").Append(vbLf).Append(vbLf)

        ' 7. footer
        sb.Append(Footer(dict, stamp)).Append(vbLf)
        sb.Append("</main>").Append(vbLf)
        sb.Append(EnhanceScript).Append(vbLf)
        sb.Append("</body>").Append(vbLf)
        sb.Append("</html>").Append(vbLf)
        Return sb.ToString()
    End Function

    Private Function ElementHtml(e As HelpElementDef, number As Integer, dict As Dictionary(Of String, String), pinned As Boolean, absent As Boolean) As String
        Dim sb As New StringBuilder()
        sb.Append("    <details class=""el"" id=""").Append(HelpContent.AnchorOf(e.Id)).Append("""").Append(If(absent, " data-absent=""1""", "")).Append(">").Append(vbLf)
        sb.Append("      <summary><span class=""mk"">").Append(number.ToString()).Append("</span><span><span class=""name"">").Append(HtmlText(PartName(dict, e)))
        sb.Append("</span><span class=""one"">").Append(HtmlText(T(dict, e.OneKey))).Append("</span></span></summary>").Append(vbLf)
        sb.Append("      <div class=""full"">").Append(vbLf)
        sb.Append("        <p>").Append(HtmlText(T(dict, e.FullKey))).Append("</p>").Append(vbLf)
        If e.NextKey <> "" OrElse e.UndoKey <> "" OrElse e.KeyText <> "" Then
            sb.Append("        <dl class=""facts"">").Append(vbLf)
            If e.NextKey <> "" Then sb.Append("          <dt>").Append(HtmlText(T(dict, "help_ui_next"))).Append("</dt><dd>").Append(HtmlText(T(dict, e.NextKey))).Append("</dd>").Append(vbLf)
            If e.UndoKey <> "" Then sb.Append("          <dt>").Append(HtmlText(T(dict, "help_ui_undo"))).Append("</dt><dd>").Append(HtmlText(T(dict, e.UndoKey))).Append("</dd>").Append(vbLf)
            If e.KeyText <> "" Then sb.Append("          <dt>").Append(HtmlText(T(dict, "help_ui_shortcut"))).Append("</dt><dd>").Append(Kbd(e.KeyText)).Append("</dd>").Append(vbLf)
            sb.Append("        </dl>").Append(vbLf)
        End If
        If e.Extra = "legend" Then sb.Append(LegendHtml(dict))
        If e.Extra = "keys" Then sb.Append(KeysHtml(dict))
        If Not pinned Then
            sb.Append("        <p class=""one""><small>").Append(HtmlText(T(dict, "help_ui_unpinned"))).Append("</small></p>").Append(vbLf)
        End If
        sb.Append("      </div>").Append(vbLf)
        sb.Append("    </details>").Append(vbLf)
        Return sb.ToString()
    End Function

    ' What a row of the Disk Manager says: the states, read from the table the list and the legend dialog use.
    Private Function LegendHtml(dict As Dictionary(Of String, String)) As String
        Dim sb As New StringBuilder()
        sb.Append("        <dl class=""facts states"">").Append(vbLf)
        For Each st In DiskHelpDialog.Legend
            sb.Append("          <dt>").Append(HtmlText(T(dict, DiskHelpDialog.StateWordKey(st)))).Append("</dt><dd>").Append(HtmlText(T(dict, DiskHelpDialog.LegendKey(st)))).Append("</dd>").Append(vbLf)
        Next
        sb.Append("        </dl>").Append(vbLf)
        Return sb.ToString()
    End Function

    ' The keyboard map, from DiskShortcuts.All - the same table the keys, the tooltips and the menus read.
    Private Function KeysHtml(dict As Dictionary(Of String, String)) As String
        Dim sb As New StringBuilder()
        sb.Append("        <dl class=""facts keys"">").Append(vbLf)
        For Each s In DiskShortcuts.All
            sb.Append("          <dt>").Append(Kbd(DiskShortcuts.KeyText(s.Chord))).Append("</dt><dd>").Append(HtmlText(T(dict, s.LabelKey))).Append("</dd>").Append(vbLf)
        Next
        sb.Append("        </dl>").Append(vbLf)
        Return sb.ToString()
    End Function

    Private Function RelatedHtml(r As HelpLinkDef, dict As Dictionary(Of String, String)) As String
        Dim href As String
        Dim pill As String
        Select Case r.Kind
            Case "window"
                href = r.Target & ".html"
                pill = "<span class=""pill"">" & HtmlText(T(dict, "help_ui_pill_window")) & "</span>"
            Case "site"
                href = Links.Site & r.Target
                pill = "<span class=""pill gold"">" & HtmlText(T(dict, "help_ui_pill_site")) & "</span>"
            Case "sibling"
                href = r.Target
                pill = "<span class=""pill gold"">" & HtmlText(T(dict, "help_ui_pill_sibling")) & "</span>"
            Case Else
                href = r.Target
                pill = "<span class=""pill gold"">" & HtmlText(T(dict, "help_ui_pill_portal")) & "</span>"
        End Select
        Return "    <li><a href=""" & HtmlText(href) & """><b>" & HtmlText(T(dict, r.TitleKey)) & "</b>" & pill & "<small>" & HtmlText(T(dict, r.NoteKey)) & "</small></a></li>" & vbLf
    End Function

    ' ---- the index -----------------------------------------------------------------

    Friend Function RenderIndex(lang As String, dict As Dictionary(Of String, String), stamp As String) As String
        Dim title = T(dict, "help_ix_title")
        Dim sb As New StringBuilder()
        sb.Append(Head(lang, title, dict))
        sb.Append("<body class=""help"">").Append(vbLf)
        sb.Append("<main class=""container"">").Append(vbLf).Append(vbLf)
        sb.Append("  <header class=""hero"">").Append(vbLf)
        sb.Append("    <p class=""kicker"">").Append(HtmlText(T(dict, "help_ui_kind_index"))).Append("</p>").Append(vbLf)
        sb.Append("    <h1>").Append(HtmlText(title)).Append("</h1>").Append(vbLf)
        sb.Append("    <p class=""lead"">").Append(HtmlText(T(dict, "help_ix_lead"))).Append("</p>").Append(vbLf)
        sb.Append("  </header>").Append(vbLf).Append(vbLf)

        sb.Append("  <h2>").Append(HtmlText(T(dict, "help_ix_main"))).Append("</h2>").Append(vbLf)
        sb.Append("  <ul class=""related"">").Append(vbLf)
        For Each p In HelpContent.Pages.Where(Function(x) x.Capture.StartsWith("shell", StringComparison.Ordinal))
            sb.Append(RelatedHtml(New HelpLinkDef("window", p.Id, p.TitleKey, IndexNoteKey(p)), dict))
        Next
        sb.Append("  </ul>").Append(vbLf).Append(vbLf)

        sb.Append("  <h2>").Append(HtmlText(T(dict, "help_ix_disks"))).Append("</h2>").Append(vbLf)
        sb.Append("  <ul class=""related"">").Append(vbLf)
        For Each p In HelpContent.Pages.Where(Function(x) Not x.Capture.StartsWith("shell", StringComparison.Ordinal))
            sb.Append(RelatedHtml(New HelpLinkDef("window", p.Id, p.TitleKey, IndexNoteKey(p)), dict))
        Next
        sb.Append("  </ul>").Append(vbLf).Append(vbLf)

        sb.Append("  <h2>").Append(HtmlText(T(dict, "help_ix_site"))).Append("</h2>").Append(vbLf)
        sb.Append("  <ul class=""related"">").Append(vbLf)
        sb.Append(RelatedHtml(New HelpLinkDef("site", "guides/", "help_ix_guides_title", "help_ix_guides_note"), dict))
        sb.Append(RelatedHtml(New HelpLinkDef("site", "guides/support.html", "help_rel_support_title", "help_rel_support_note"), dict))
        sb.Append(RelatedHtml(New HelpLinkDef("site", "privacy.html", "help_rel_privacy_title", "help_rel_privacy_note"), dict))
        sb.Append("  </ul>").Append(vbLf).Append(vbLf)

        sb.Append("  <footer class=""help-foot"">").Append(vbLf)
        sb.Append("    <span>").Append(HtmlText(Localization.Format(T(dict, "help_ui_captured"), VersionOf(stamp), DateOf(stamp)))).Append("</span>").Append(vbLf)
        sb.Append("    <a href=""").Append(HtmlText(Links.Site & "guides/support.html")).Append(""">").Append(HtmlText(T(dict, "help_ui_support"))).Append("</a>").Append(vbLf)
        sb.Append("  </footer>").Append(vbLf)
        sb.Append("</main>").Append(vbLf)
        sb.Append("</body>").Append(vbLf)
        sb.Append("</html>").Append(vbLf)
        Return sb.ToString()
    End Function

    ' The one-line note an index entry carries: the note other pages use for it in Related.
    Private Function IndexNoteKey(p As HelpPageDef) As String
        Select Case p.Id
            Case "job-page" : Return "help_rel_jobpage_note"
            Case "command" : Return "help_rel_command_note"
            Case "history" : Return "help_rel_history_note"
            Case "settings" : Return "help_rel_settings_note"
            Case "about" : Return "help_rel_about_note"
        End Select
        Return "help_ix_dm_note"
    End Function
End Module
