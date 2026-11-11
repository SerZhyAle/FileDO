Imports System.IO
Imports System.Threading

' The pictures of the help pages (SP-0168; CONTEXT-HELP rule 14): taken off screen from the real window,
' with the position of each numbered marker read from the window's own control tree - so a layout change
' regenerates correct markers and no one has to place a pin by hand.
'
' Like Capture.vb, which draws the guides' pictures, nothing here touches the user's settings (HelpPages
' holds the settings off HKCU for the whole run) and the content is demo content: the Disk Manager is
' built from a sample `vd status json` document, the History page from sample reports, a job page opened
' on the system drive. The author's disks, paths and reports never appear.
Friend Module HelpCapture

    ' Design pixels, so a picture has the same layout at any display scale.
    Private Const DesignWidth As Integer = 1180
    Private Const DesignHeight As Integer = 760

    ' The picture of a page's window in the palette asked for, written to file; the markers found in it.
    Friend Function Take(page As HelpPageDef, dark As Boolean, file As String) As HelpShot
        Theme.UsePaletteForTest(dark)
        Dim parts = page.Capture.Split("|"c)
        Select Case parts(0)
            Case "shell"
                Return TakeShell(page, parts(1), If(parts.Length > 2, parts(2), ""), SizeOf(parts, 3, DesignWidth, DesignHeight), file)
            Case "disk-manager"
                Return TakeDiskManager(page, SizeOf(parts, 1, 1400, 700), file)
        End Select
        Throw New InvalidOperationException("no capture recipe for " & page.Capture)
    End Function

    ' The design size a recipe asks for in its part `index` ("1180x1500"), or the default one. A page
    ' that is taller than the usual window (Settings, with every group open) asks for the height it needs,
    ' so that every part is in the picture.
    Private Function SizeOf(parts As String(), index As Integer, defaultWidth As Integer, defaultHeight As Integer) As Size
        If parts.Length > index Then
            Dim xy = parts(index).Split("x"c)
            Dim w As Integer, h As Integer
            If xy.Length = 2 AndAlso Integer.TryParse(xy(0), w) AndAlso Integer.TryParse(xy(1), h) Then Return New Size(w, h)
        End If
        Return New Size(defaultWidth, defaultHeight)
    End Function

    Private Sub Settle()
        For i = 1 To 6
            Application.DoEvents()
            Thread.Sleep(40)
        Next
    End Sub

    Private Function TakeShell(page As HelpPageDef, railKey As String, target As String, design As Size, file As String) As HelpShot
        Dim shell As New ShellForm()
        Try
            shell.StartPosition = FormStartPosition.Manual
            shell.WindowState = FormWindowState.Normal
            shell.ShowInTaskbar = False
            shell.Bounds = New Rectangle(-32000, -32000, Ui.Px(shell, design.Width), Ui.Px(shell, design.Height))
            shell.Show()
            shell.ShowPageForCapture(railKey, target)
            shell.PrepareForHelpCapture(railKey)
            Settle()
            Dim client = shell.ClientRectangle
            Using bmp As New Bitmap(client.Width, client.Height)
                shell.DrawClientForCapture(bmp)
                bmp.Save(file, Imaging.ImageFormat.Png)
            End Using
            Return PinsFor(page, shell, client.Size)
        Finally
            shell.Dispose()
        End Try
    End Function

    Private Function TakeDiskManager(page As HelpPageDef, design As Size, file As String) As HelpShot
        DiskManagerForm.SuppressWelcome = True
        DiskManagerForm.SuppressReads = True
        Dim m As New DiskManagerForm()
        Try
            m.StartPosition = FormStartPosition.Manual
            m.ShowInTaskbar = False
            m.Bounds = New Rectangle(-32000, -32000, Ui.Px(m, design.Width), Ui.Px(m, design.Height))
            m.Show()
            Dim problem As String = ""
            Dim snap = DiskSnapshot.Parse(Capture.SampleSnapshotLine(), problem)
            m.ApplySnapshotForTest(snap, "")
            m.SelectForTest(snap.Disks.First(Function(d) d.Name = "secrets").Key)
            Settle()
            Dim client = m.ClientRectangle
            Using bmp As New Bitmap(client.Width, client.Height)
                m.DrawClientForCapture(bmp)
                ' A native list view does not print: the rows are drawn by the painters the list uses.
                Using g = Graphics.FromImage(bmp)
                    m.PaintListForCapture(g, m.ListOriginForCapture())
                End Using
                bmp.Save(file, Imaging.ImageFormat.Png)
            End Using
            Return PinsFor(page, m, client.Size)
        Finally
            m.Dispose()
        End Try
    End Function

    ' ---- the markers --------------------------------------------------------------

    ' The control with this name that is on screen. The views of a window name their parts alike ("help:run"
    ' is on the job page and on the Command page), and only the one in front is the one the picture shows.
    Friend Function FindByName(root As Control, name As String) As Control
        If root.Name = name AndAlso root.Visible Then Return root
        For Each c As Control In root.Controls
            Dim hit = FindByName(c, name)
            If hit IsNot Nothing Then Return hit
        Next
        Return Nothing
    End Function

    ' A marker stands on the top-left corner of its element, where it hides the least of it and where an
    ' eye looks for it. An element that is not on
    ' screen - a Stop button while nothing runs - has no marker, and its entry says so.
    Private Function PinsFor(page As HelpPageDef, form As Form, size As Size) As HelpShot
        Dim shot As New HelpShot With {.Width = size.Width, .Height = size.Height}
        Dim whole As New Rectangle(Point.Empty, size)
        Dim taken As New List(Of Point)
        Dim apart = Ui.Px(form, 34)
        For Each e In page.Elements
            If e.Locator = "" Then Continue For
            Dim c = FindByName(form, e.Locator)
            If c Is Nothing OrElse Not c.Visible Then Continue For
            Dim r = Rectangle.Intersect(form.RectangleToClient(c.RectangleToScreen(c.ClientRectangle)), whole)
            ' Squeezed out of the picture in this language: recorded as absent, never pinned (CONTEXT-HELP 0.2 section 7 item C).
            If r.Width < 6 OrElse r.Height < 6 Then
                shot.Absent.Add(e.Id)
                Continue For
            End If
            Dim x = r.Left + Ui.Px(form, 2)
            Dim y = Math.Max(r.Top, Ui.Px(form, 8))
            Dim moved = True
            Dim guard = 0
            While moved AndAlso guard < 12
                moved = False
                guard += 1
                For Each q In taken
                    If Math.Abs(q.X - x) < apart AndAlso Math.Abs(q.Y - y) < apart Then
                        y = q.Y + apart
                        moved = True
                    End If
                Next
            End While
            y = Math.Min(y, size.Height - Ui.Px(form, 8))
            taken.Add(New Point(x, y))
            shot.Pins.Add(New HelpPin With {.ElementId = e.Id, .X = 100.0 * x / size.Width, .Y = 100.0 * y / size.Height})
        Next
        Return shot
    End Function
End Module
