' The window icon for every FileDO window - title bar, taskbar, Alt+Tab.
'
' ApplicationIcon only stamps the icon Explorer shows on the .exe; a WinForms Form with no
' Form.Icon falls back to the default .NET window icon. Both windows therefore ask for it here.
'
' assets\icon.ico is linked into the assembly as FileDOGUI.icon.ico (see FileDOGUI.vbproj) so the
' full multi-size icon travels inside the exe and Windows can pick the size it needs. If that
' resource is ever missing, the icon already stamped on the exe is used instead.

Imports System.Reflection

Module AppIcon

    Private Const ResourceName As String = "FileDOGUI.icon.ico"

    Private ReadOnly appIconValue As Icon = Load()

    ''' <summary>Gives the form the application icon. A no-op if the icon could not be loaded.</summary>
    Friend Function SystemIcon() As Icon
        Return appIconValue
    End Function

    Public Sub Apply(form As Form)
        If form IsNot Nothing AndAlso appIconValue IsNot Nothing Then form.Icon = appIconValue
    End Sub

    ''' <summary>The product mark as a picture of the given size, for a button that stands for the product itself (ICON-RENDER rule 9: the mark, not a glyph).</summary>
    Public Function Mark(px As Integer) As Bitmap
        If appIconValue Is Nothing OrElse px <= 0 Then Return Nothing
        Try
            Using sized As New Icon(appIconValue, New Size(px, px))
                Return sized.ToBitmap()
            End Using
        Catch
            Return Nothing
        End Try
    End Function

    Private Function Load() As Icon
        Try
            Using stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(ResourceName)
                If stream IsNot Nothing Then Return New Icon(stream)
            End Using
        Catch
            ' Fall through to the icon on the exe.
        End Try
        Try
            Return Icon.ExtractAssociatedIcon(Application.ExecutablePath)
        Catch
            Return Nothing
        End Try
    End Function

End Module
