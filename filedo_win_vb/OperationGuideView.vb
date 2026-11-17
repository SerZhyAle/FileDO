' The About operations page (SP-0204; renamed from Commands): every operation of the Command page's list under
' the heading of its group. Each shows its name and its one-line description; a click on the name opens the
' full description, a line a person could type and a button that opens the Command page with that operation
' chosen. A search box at the top narrows the list by the name or by any word of the descriptions. It is the
' place to read about the operations outside of choosing one; the dialog that opens from Browse commands.. is
' the place while choosing.
'
' The name is the header of a collapsible group (GroupHeader, COLLAPSIBLE-GROUP): the whole band is one hit
' target that toggles it, Enter and Space do the same from the keyboard, and the page reports the state
' to a screen reader. Every entry starts collapsed, so thirty-one operations read as a short list.
'
' The page is a page of the shell like About: a scrolling stack of cards. It is built on its first visit
' (EnsureBuilt), because thirty-one entries of labels and buttons are not something the shell's start should
' pay for when most sessions never open the page. The words come from the tables of the language in force
' when it is built; a live language switch afterwards rebinds the texts by value (LiveLanguage), which is why
' the example line is one {0} template and not a caption glued to a command.
Public Class OperationGuideView
    Inherits UserControl

    ' Use this command on an entry: the shell opens the Command page with that word chosen.
    Public Event UseRequested(word As String)

    Private dict As Dictionary(Of String, String)
    Private built As Boolean = False

    Private root As TableLayoutPanel
    Private introCard As ShellCard
    Private introLabel As Label
    Private searchLabel As Label
    Private searchBox As TextBox
    Private noMatchLabel As Label
    Private noMatchShown As Boolean = False
    Private ReadOnly cards As New List(Of ShellCard)
    Private ReadOnly sections As New List(Of GuideSection)
    Private ReadOnly entries As New Dictionary(Of String, GuideEntry)(StringComparer.Ordinal)

    ' One operation as the page holds it: its parts, and whether its full description is open.
    Private NotInheritable Class GuideEntry
        Public Word As String
        Public Block As TableLayoutPanel
        Public Header As GroupHeader
        Public Brief As Label
        Public Detail As TableLayoutPanel
        Public Full As Label
        Public Example As Label
        Public UseButton As GlyphButton
        Public Expanded As Boolean = False
        ' Whether the search leaves the entry on the page. Control.Visible also folds in the parents, so the
        ' page keeps its own answer for the self-test.
        Public Shown As Boolean = True

        ' Every search word is found in the name and the one-line description.
        Public Function HeadMatches(tokens As String()) As Boolean
            Return ContainsAll(Word & " " & Brief.Text, tokens)
        End Function

        ' Every search word is found somewhere in the entry: name, one line, full text or example.
        Public Function Matches(tokens As String()) As Boolean
            Return ContainsAll(Word & " " & Brief.Text & " " & Full.Text & " " & Example.Text, tokens)
        End Function

        Private Shared Function ContainsAll(haystack As String, tokens As String()) As Boolean
            For Each token In tokens
                If haystack.IndexOf(token, StringComparison.CurrentCultureIgnoreCase) < 0 Then Return False
            Next
            Return True
        End Function
    End Class

    ' One group heading with the card that holds its entries.
    Private NotInheritable Class GuideSection
        Public Head As Label
        Public Card As ShellCard
        Public ReadOnly Members As New List(Of GuideEntry)
        Public Shown As Boolean = True
    End Class

    Public Sub New()
        DoubleBuffered = True
        Dock = DockStyle.Fill
        AutoScroll = True
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Function LText(key As String) As String
        Return Localization.Multiline(L(key))
    End Function

    ' Builds the entries once, themed, the first time the page is shown.
    Friend Sub EnsureBuilt()
        If built Then Return
        built = True
        dict = Localization.GetDict(ShellSettings.Language())
        BuildLayout()
        ApplyTheme()
    End Sub

    Private Sub AddRow(c As Control)
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.Controls.Add(c, 0, root.RowStyles.Count - 1)
    End Sub

    Private Sub BuildLayout()
        SuspendLayout()

        root = New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Padding = Ui.PxPad(Me, 0, 0, 0, 16),
            .Margin = New Padding(0)
        }
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        introCard = New ShellCard With {
            .Padding = Ui.PxPad(Me, 18, 16, 18, 16),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
        }
        Dim introStack As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Margin = New Padding(0)
        }
        introStack.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        introLabel = New Label With {.Text = LText("opguide_intro"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 12)}
        searchLabel = New Label With {.Text = L("opguide_search"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        searchBox = New TextBox With {.Dock = DockStyle.Fill, .Margin = Ui.PxPad(Me, 0, 2, 0, 4)}
        searchBox.Name = "opguide:search"
        searchBox.AccessibleName = L("opguide_search")
        AddHandler searchBox.TextChanged, Sub() ApplyFilter()
        AddHandler searchBox.KeyDown, AddressOf SearchBox_KeyDown
        noMatchLabel = New Label With {.Text = L("opguide_search_none"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 0), .Visible = False}

        introStack.Controls.Add(introLabel, 0, 0)
        introStack.Controls.Add(searchLabel, 0, 1)
        introStack.Controls.Add(searchBox, 0, 2)
        introStack.Controls.Add(noMatchLabel, 0, 3)
        introCard.Controls.Add(introStack)
        cards.Add(introCard)
        AddRow(introCard)
        Ui.Wrap(introLabel, introCard, Ui.Px(Me, 36))
        Ui.Wrap(searchLabel, introCard, Ui.Px(Me, 36))
        Ui.Wrap(noMatchLabel, introCard, Ui.Px(Me, 36))

        For Each group In OperationGuide.Groups
            Dim section As New GuideSection()
            section.Head = New Label With {.Text = L(OperationGuide.GroupKey(group)), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 8)}
            AddRow(section.Head)

            section.Card = New ShellCard With {
                .Padding = Ui.PxPad(Me, 18, 16, 18, 0),
                .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
            }
            Dim stack As New TableLayoutPanel With {
                .Dock = DockStyle.Top,
                .AutoSize = True,
                .AutoSizeMode = AutoSizeMode.GrowAndShrink,
                .ColumnCount = 1,
                .Margin = New Padding(0)
            }
            stack.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
            For Each entry In OperationGuide.EntriesOf(group)
                section.Members.Add(AddEntry(stack, section.Card, entry))
            Next
            section.Card.Controls.Add(stack)
            cards.Add(section.Card)
            AddRow(section.Card)
            sections.Add(section)
        Next

        Controls.Add(root)
        ResumeLayout(True)
    End Sub

    ' One operation: its name as a collapsible header, what it does in a line, and - behind the header - what it
    ' does in full, an example and the button.
    Private Function AddEntry(stack As TableLayoutPanel, card As ShellCard, entry As OperationEntry) As GuideEntry
        Dim parts As New GuideEntry With {.Word = entry.Word}

        parts.Block = New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14),
            .Name = "opguide:" & entry.Word,
            .Tag = entry.Word
        }
        parts.Block.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        parts.Header = New GroupHeader With {
            .Name = "opguide:" & entry.Word & ":header",
            .Text = entry.Word,
            .Dock = DockStyle.Top,
            .TabStop = True,
            .Margin = New Padding(0),
            .Height = Ui.Px(Me, UiTokens.GroupHeaderHeight()),
            .Collapsed = True
        }
        parts.Header.AccessibleName = entry.Word
        parts.Header.AccessibleDescription = L("rail_group_collapsed")
        parts.Header.DefaultActionText = L("rail_group_expand")
        AddHandler parts.Header.SizeChanged, Sub() parts.Header.Height = parts.Header.PreferredHeightFor(parts.Header.Width)
        AddHandler parts.Header.Click, Sub() SetExpanded(parts, Not parts.Expanded)

        parts.Brief = New Label With {.Text = L(entry.ShortKey), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2), .Cursor = Cursors.Hand}
        AddHandler parts.Brief.Click, Sub() SetExpanded(parts, Not parts.Expanded)

        parts.Detail = New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Margin = New Padding(0),
            .Visible = False
        }
        parts.Detail.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        parts.Full = New Label With {.Text = LText(entry.FullKey), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 8)}
        parts.Example = New Label With {
            .Text = Localization.Format(L("opguide_example_fmt"), entry.Example),
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 8)
        }
        parts.UseButton = Ui.GlyphCommandButton(L("opguide_use"), GlyphRef.Vocabulary("app.command-line"), Ui.PxPad(Me, 0, 0, 0, 4))
        parts.UseButton.Tag = entry.Word
        AddHandler parts.UseButton.Click, AddressOf UseButton_Click

        parts.Detail.Controls.Add(parts.Full, 0, 0)
        parts.Detail.Controls.Add(parts.Example, 0, 1)
        parts.Detail.Controls.Add(parts.UseButton, 0, 2)

        parts.Block.Controls.Add(parts.Header, 0, 0)
        parts.Block.Controls.Add(parts.Brief, 0, 1)
        parts.Block.Controls.Add(parts.Detail, 0, 2)
        stack.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        stack.Controls.Add(parts.Block, 0, stack.RowStyles.Count - 1)

        Ui.Wrap(parts.Brief, card, Ui.Px(Me, 36))
        Ui.Wrap(parts.Full, card, Ui.Px(Me, 36))
        Ui.Wrap(parts.Example, card, Ui.Px(Me, 36))

        entries(entry.Word) = parts
        Return parts
    End Function

    ' Opens or closes an entry's full description. Closing with the focus inside it hands the focus to the header,
    ' which stays where it was.
    Private Sub SetExpanded(parts As GuideEntry, expanded As Boolean)
        If parts.Expanded = expanded Then Return
        If Not expanded AndAlso parts.Detail.ContainsFocus Then parts.Header.Focus()
        parts.Expanded = expanded
        parts.Header.Collapsed = Not expanded
        parts.Detail.Visible = expanded
        parts.Header.AccessibleDescription = L(If(expanded, "rail_group_expanded", "rail_group_collapsed"))
        parts.Header.DefaultActionText = L(If(expanded, "rail_group_collapse", "rail_group_expand"))
    End Sub

    Private Sub UseButton_Click(sender As Object, e As EventArgs)
        Dim c = TryCast(sender, Control)
        If c Is Nothing OrElse c.Tag Is Nothing Then Return
        RaiseEvent UseRequested(CStr(c.Tag))
    End Sub

    ' ---- search ----------------------------------------------------------

    Private Shared Function SearchTokens(text As String) As String()
        Return If(text, "").Split(New Char() {" "c, ControlChars.Tab}, StringSplitOptions.RemoveEmptyEntries)
    End Function

    ' Shows the entries that hold every word typed in the search box, under the headings that still have one,
    ' and nothing narrowed when the box is empty. An entry that matches only through its full description or its
    ' example is opened, so the words that found it are in front of the reader; an entry the person opened is
    ' never closed by a search.
    Private Sub ApplyFilter()
        If Not built Then Return
        Dim tokens = SearchTokens(searchBox.Text)
        Dim shown = 0
        Ui.SuspendTree(Me)
        Try
            For Each section In sections
                Dim any = False
                For Each parts In section.Members
                    Dim visible = tokens.Length = 0 OrElse parts.Matches(tokens)
                    parts.Shown = visible
                    parts.Block.Visible = visible
                    If visible Then
                        any = True
                        shown += 1
                        If tokens.Length > 0 AndAlso Not parts.HeadMatches(tokens) Then SetExpanded(parts, True)
                    End If
                Next
                section.Shown = any
                section.Head.Visible = any
                section.Card.Visible = any
            Next
            noMatchShown = (shown = 0)
            noMatchLabel.Visible = noMatchShown
        Finally
            Ui.ResumeTree(Me)
        End Try
    End Sub

    ' Escape empties the box.
    Private Sub SearchBox_KeyDown(sender As Object, e As KeyEventArgs)
        If e.KeyCode = Keys.Escape AndAlso searchBox.Text <> "" Then
            searchBox.Text = ""
            e.Handled = True
            e.SuppressKeyPress = True
        End If
    End Sub

    ' Ctrl+F from anywhere on the page puts the cursor in the search box.
    Protected Overrides Function ProcessCmdKey(ByRef msg As Message, keyData As Keys) As Boolean
        If built AndAlso keyData = (Keys.Control Or Keys.F) Then
            searchBox.Focus()
            searchBox.SelectAll()
            Return True
        End If
        Return MyBase.ProcessCmdKey(msg, keyData)
    End Function

    ' Brings an operation's entry into view, open, with the focus on its button - where the dialog's Full
    ' descriptions button leads. A search in the box is emptied first, so the entry is on the page.
    Friend Sub ShowOperation(word As String)
        EnsureBuilt()
        Dim parts As GuideEntry = Nothing
        If Not entries.TryGetValue(word, parts) Then Return
        If searchBox.Text <> "" Then searchBox.Text = ""
        SetExpanded(parts, True)
        root.PerformLayout()
        ScrollControlIntoView(parts.Block)
        ActiveControl = parts.UseButton
    End Sub

    Public Sub ApplyTheme()
        If Not built Then Return
        Ui.SuspendTree(Me)
        Try
            ApplyThemeCore()
        Finally
            Ui.ResumeTree(Me)
        End Try
    End Sub

    Private Sub ApplyThemeCore()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        For Each card In cards
            card.BackColor = p.Surface
            card.BorderColour = p.Border
        Next

        For Each lbl As Label In New Label() {introLabel, searchLabel}
            lbl.Font = Theme.FontBody()
            lbl.ForeColor = p.Text
        Next
        searchBox.Font = Theme.FontBody()
        searchBox.BackColor = p.SurfaceAlt
        searchBox.ForeColor = p.Text
        searchBox.BorderStyle = BorderStyle.FixedSingle
        noMatchLabel.Font = Theme.FontBody()
        noMatchLabel.ForeColor = p.MutedText

        For Each section In sections
            section.Head.Font = Theme.FontSubtitle()
            section.Head.ForeColor = p.Accent
            For Each parts In section.Members
                parts.Brief.Font = Theme.FontBody()
                parts.Brief.ForeColor = p.MutedText
                parts.Full.Font = Theme.FontBody()
                parts.Full.ForeColor = p.Text
                parts.Example.Font = Theme.FontMono()
                parts.Example.ForeColor = p.MutedText
                parts.UseButton.Font = Theme.FontBody()
            Next
        Next

        Invalidate(True)
    End Sub

    ' For SelfTest.vb: whether the page is built, the entries it holds, and a click on an entry's button.
    Friend ReadOnly Property BuiltForTest As Boolean
        Get
            Return built
        End Get
    End Property

    Friend ReadOnly Property EntryWordsForTest As List(Of String)
        Get
            Return entries.Keys.ToList()
        End Get
    End Property

    ' The button sits behind the header, so a click on it opens the entry first, as a person's would.
    Friend Sub UseForTest(word As String)
        Dim parts As GuideEntry = Nothing
        If Not entries.TryGetValue(word, parts) Then Return
        SetExpanded(parts, True)
        UseButton_Click(parts.UseButton, EventArgs.Empty)
    End Sub

    ' A click on an entry's header.
    Friend Sub ToggleForTest(word As String)
        Dim parts As GuideEntry = Nothing
        If entries.TryGetValue(word, parts) Then parts.Header.PerformClick()
    End Sub

    Friend Function IsExpandedForTest(word As String) As Boolean
        Dim parts As GuideEntry = Nothing
        Return entries.TryGetValue(word, parts) AndAlso parts.Expanded
    End Function

    Friend Sub SearchForTest(text As String)
        searchBox.Text = text
    End Sub

    ' The entries the search leaves on the page, in page order.
    Friend Function ShownWordsForTest() As List(Of String)
        Dim words As New List(Of String)
        For Each section In sections
            If Not section.Shown Then Continue For
            For Each parts In section.Members
                If parts.Shown Then words.Add(parts.Word)
            Next
        Next
        Return words
    End Function

    Friend ReadOnly Property NoMatchShownForTest As Boolean
        Get
            Return noMatchShown
        End Get
    End Property

    Friend Function TextsForTest() As List(Of String)
        Dim texts As New List(Of String)
        For Each c As Control In AllChildren(Me)
            If TypeOf c Is Label OrElse TypeOf c Is Button Then texts.Add(c.Text)
        Next
        Return texts
    End Function

    Private Iterator Function AllChildren(parent As Control) As IEnumerable(Of Control)
        For Each c As Control In parent.Controls
            Yield c
            For Each inner In AllChildren(c)
                Yield inner
            Next
        Next
    End Function

End Class
