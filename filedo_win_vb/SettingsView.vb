Public Class SettingsView
    Inherits UserControl
    Public Event ThemeChanged()
    Friend ReadOnly Panel As SettingsPanel
    Public Sub New()
        Dock = DockStyle.Fill
        Panel = New SettingsPanel()
        Controls.Add(Panel)
        AddHandler Panel.ThemeChanged, Sub() RaiseEvent ThemeChanged()
    End Sub
    Public Sub ApplyTheme()
        BackColor = Theme.Current.Background
        Panel.ApplyTheme()
    End Sub
End Class
