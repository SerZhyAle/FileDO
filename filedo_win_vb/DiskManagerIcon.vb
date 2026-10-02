' The Disk Manager specific icon for window, shortcut, tray, and button use.
'
Imports System.Reflection

Module DiskManagerIcon

    Private Const ResourceName As String = "FileDOGUI.app.disk-manager.ico"

    Private ReadOnly diskManagerIconValue As Icon = Load()

    ''' <summary>Gives the Disk Manager form its dedicated icon.</summary>
    Friend Function DiskManagerIcon() As Icon
        Return diskManagerIconValue
    End Function

    ''' <summary>Applies the Disk Manager icon to the given form.</summary>
    Public Sub Apply(form As Form)
        If form IsNot Nothing AndAlso diskManagerIconValue IsNot Nothing Then form.Icon = diskManagerIconValue
    End Sub

    ''' <summary>The Disk Manager mark as a picture of the given size, for buttons.</summary>
    Public Function Mark(px As Integer) As Bitmap
        If diskManagerIconValue Is Nothing OrElse px <= 0 Then Return Nothing
        Try
            Using sized As New Icon(diskManagerIconValue, New Size(px, px))
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
            ' Fall through to nothing.
        End Try
        Return Nothing
    End Function

End Module
