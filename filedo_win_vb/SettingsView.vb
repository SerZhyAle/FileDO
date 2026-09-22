' The settings page: the three switches the specification actually asks for, and nothing else.
'
'   D13 - the theme follows Windows, with a manual override.
'   D14 - the language, which is the same GuiLang value the builder reads and writes; one program,
'         one language (section 13).
'   D8  - the owner's addition: automatic run reports can be turned off entirely.
'
' What is deliberately absent is any "do not ask me again" for a destructive path. Section 8 item 4
' forbids it in every form, including a settings toggle, so there is nowhere on this page it could
' be added by accident.
Public Class SettingsView
    Inherits UserControl

    Public Event ThemeChanged()

    Private ReadOnly dict As Dictionary(Of String, String)
    Private card As ShellCard
    Private themeHeader As Label
    Private themeCombo As ComboBox
    Private langHeader As Label
    Private langCombo As ComboBox
    Private langHint As Label
    Private historyHeader As Label
    Private historyCheck As CheckBox
    Private historyHint As Label
    Private suppress As Boolean = False

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        DoubleBuffered = True
        Dock = DockStyle.Fill
        AutoScroll = True
        BuildLayout()
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Sub BuildLayout()
        SuspendLayout()

        Dim root As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 1,
            .Padding = Ui.PxPad(Me, 0, 0, 0, 16),
            .Margin = New Padding(0)
        }
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        card = New ShellCard With {
            .Padding = Ui.PxPad(Me, 18, 16, 18, 16),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
        }

        Dim t As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 9,
            .Margin = New Padding(0)
        }
        t.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        themeHeader = New Label With {.Text = L("shell_settings_theme"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
        themeCombo = New ComboBox With {
            .DropDownStyle = ComboBoxStyle.DropDownList,
            .Width = Ui.Px(Me, 260),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
        }
        themeCombo.AccessibleName = L("shell_settings_theme")
        themeCombo.Items.Add(L("shell_theme_auto"))
        themeCombo.Items.Add(L("shell_theme_light"))
        themeCombo.Items.Add(L("shell_theme_dark"))
        themeCombo.SelectedIndex = ThemeIndex(ShellSettings.ThemeChoice())
        AddHandler themeCombo.SelectedIndexChanged, AddressOf ThemeCombo_Changed

        langHeader = New Label With {.Text = L("shell_settings_lang"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
        langCombo = New ComboBox With {
            .DropDownStyle = ComboBoxStyle.DropDownList,
            .Width = Ui.Px(Me, 260),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 4)
        }
        langCombo.AccessibleName = L("shell_settings_lang")
        For Each langName In Localization.LanguageNames
            langCombo.Items.Add(langName)
        Next
        langCombo.SelectedIndex = Math.Max(0, Array.IndexOf(Localization.Languages, ShellSettings.Language()))
        AddHandler langCombo.SelectedIndexChanged, AddressOf LangCombo_Changed

        langHint = New Label With {.Text = L("shell_settings_lang_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 14)}

        historyHeader = New Label With {.Text = L("shell_settings_history_title"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
        historyCheck = New CheckBox With {
            .Text = L("shell_settings_history"),
            .AutoSize = True,
            .Checked = ShellSettings.HistoryEnabled(),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 4)
        }
        AddHandler historyCheck.CheckedChanged, Sub() ShellSettings.SetHistoryEnabled(historyCheck.Checked)

        historyHint = New Label With {.Text = L("shell_settings_history_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 0)}

        t.Controls.Add(themeHeader, 0, 0)
        t.Controls.Add(themeCombo, 0, 1)
        t.Controls.Add(langHeader, 0, 2)
        t.Controls.Add(langCombo, 0, 3)
        t.Controls.Add(langHint, 0, 4)
        t.Controls.Add(historyHeader, 0, 5)
        t.Controls.Add(historyCheck, 0, 6)
        t.Controls.Add(historyHint, 0, 7)

        card.Controls.Add(t)
        root.Controls.Add(card, 0, 0)
        Controls.Add(root)

        Ui.Wrap(langHint, card, Ui.Px(Me, 36))
        Ui.Wrap(historyHint, card, Ui.Px(Me, 36))

        ResumeLayout(True)
    End Sub

    Private Shared Function ThemeIndex(choice As String) As Integer
        Select Case choice
            Case "light" : Return 1
            Case "dark" : Return 2
            Case Else : Return 0
        End Select
    End Function

    Private Sub ThemeCombo_Changed(sender As Object, e As EventArgs)
        If suppress Then Return
        Dim choice = "auto"
        Select Case themeCombo.SelectedIndex
            Case 1 : choice = "light"
            Case 2 : choice = "dark"
        End Select
        ShellSettings.SetThemeChoice(choice)
        RaiseEvent ThemeChanged()
    End Sub

    ' The language is stored now and shown on the next opening: every view builds its dictionary
    ' once, at construction, so re-labelling a live window would leave half of it in the old
    ' language - which is worse than asking for a restart and saying so.
    Private Sub LangCombo_Changed(sender As Object, e As EventArgs)
        If suppress Then Return
        Dim i = langCombo.SelectedIndex
        If i >= 0 AndAlso i < Localization.Languages.Length Then
            ShellSettings.SetLanguage(Localization.Languages(i))
        End If
    End Sub

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        card.BackColor = p.Surface
        card.BorderColour = p.Border

        For Each h As Label In New Label() {themeHeader, langHeader, historyHeader}
            h.Font = Theme.FontSubtitle()
            h.ForeColor = p.Accent
        Next

        For Each c As ComboBox In New ComboBox() {themeCombo, langCombo}
            c.Font = Theme.FontBody()
            c.BackColor = p.SurfaceAlt
            c.ForeColor = p.Text
            c.FlatStyle = FlatStyle.Flat
        Next

        historyCheck.Font = Theme.FontBody()
        historyCheck.ForeColor = p.Text

        For Each lbl As Label In New Label() {langHint, historyHint}
            lbl.Font = Theme.FontCaption()
            lbl.ForeColor = p.MutedText
        Next

        suppress = True
        themeCombo.SelectedIndex = ThemeIndex(ShellSettings.ThemeChoice())
        suppress = False

        Invalidate(True)
    End Sub

End Class
