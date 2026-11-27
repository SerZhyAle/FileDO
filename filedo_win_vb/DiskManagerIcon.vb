' The Disk Manager specific icon for window, taskbar button and shortcut use: the plated
' content.disk-container icon (MenuIcons.vb, "app.disk-manager"). It is read from the copy of
' assets\menu-icons\app.disk-manager.ico this exe embeds, so the window and the MSI's shortcut
' (which names the same file) cannot differ. The shell's rail row that opens the window does not
' draw it: a rail has one glyph treatment (owner decision 2026-11-26, FILEDO-0250).
'
Module DiskManagerIcon

    Private Const IconId As String = "app.disk-manager"

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
            Dim bytes = MenuIcons.EmbeddedIco(IconId)
            If bytes IsNot Nothing Then
                Using stream As New IO.MemoryStream(bytes)
                    Return New Icon(stream)
                End Using
            End If
        Catch
            ' Fall through to nothing.
        End Try
        Return Nothing
    End Function

End Module
