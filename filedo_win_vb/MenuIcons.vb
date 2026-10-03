' The icons of FileDO's Explorer surfaces (SP-0016 T8, decision D3): each entry of the "File DO.."
' menu and the .fd-sec document type show their meaning's glyph, not the product mark (ICON-SET
' rule 7 - the group entry keeps the mark, ICON-RENDER rule 9).
'
' Explorer draws these from static .ico files, on a menu that is light or dark by the user's
' setting and never asks FileDO which, so the theme colour of ICON-RENDER rule 2 cannot be used.
' ICON-RENDER 0.12 rule 9 settles it: the mono look in one tone, #808080, which holds 3:1 on both
' menus. The files are drawn here, from the same vendored catalog drawings the shell paints, so an
' icon cannot drift from its glyph:
'
'   filedo_win.exe --write-menu-icons <folder>   writes <id>.ico for every id below
'
' and the result is kept in assets\menu-icons\, where the MSI, the release zip, the MSIX and
' `filedo fdsec register` take it from (the three writers name the files icons\<id>.ico beside the
' exe). The copies are embedded in this exe as well, and --selftest draws every icon afresh and
' compares it with its copy, pixel by pixel, so a changed drawing that nobody re-wrote fails the gate.
'
' A second kind of icon lives here: the PLATED one, the product's own picture of a window rather
' than an Explorer entry's meaning. "app.disk-manager" is the content.disk-container glyph on a
' rounded-square plate in the accent, the glyph in the on-plate colour (ICON-RENDER 0.14 section 3
' rule 6 and section 10 items D and E; owner decision 2026-10-02). It is ONE icon wherever the Disk
' Manager appears - its window and taskbar button, its launcher in the shell (DiskManagerIcon), and
' the MSI's Start menu shortcut - and it is written, embedded and self-tested exactly like the mono
' ones. The generic mono content.disk-container icon stays the .fdd file type's and the Explorer
' menu's, and the F|D mark (assets\icon.ico) stays the exe's and the installer's.
Imports System.Drawing.Drawing2D
Imports System.Drawing.Imaging
Imports System.IO

Public Module MenuIcons

    ' The meanings the Explorer surfaces show: the ten menu entries share five, the .fd-sec document
    ' type is the sixth, and the .fdd disk container type (SP-0004 6.1, ICON-SET 0.16) the seventh.
    ' Which entry takes which is the writers' table (fdsecMenuItems, FileDO.wxs, FileDOShell.cpp),
    ' held equal by their parity tests.
    Public ReadOnly Ids As String() = {
        "action.secure", "action.unsecure", "action.wipe", "action.verify", "app.info", "content.secret-file",
        "content.disk-container"
    }

    ' The plated icons (see the header): id -> the vocabulary glyph drawn on the plate. Ids above stays
    ' the mono meanings only - the rest of the shell reads it as "vocabulary ids".
    Public ReadOnly PlatedIds As String() = {"app.disk-manager"}

    Private ReadOnly PlatedGlyphs As New Dictionary(Of String, String)(StringComparer.Ordinal) From {
        {"app.disk-manager", "content.disk-container"}
    }

    ' Every icon --write-menu-icons writes and the exe embeds: the mono ones, then the plated ones.
    Public ReadOnly Property AllIds As String()
        Get
            Return Ids.Concat(PlatedIds).ToArray()
        End Get
    End Property

    Public Function IsPlated(id As String) As Boolean
        Return PlatedGlyphs.ContainsKey(id)
    End Function

    ' The vocabulary meaning a plated icon carries.
    Public Function PlatedGlyphId(id As String) As String
        Dim glyph As String = Nothing
        Return If(PlatedGlyphs.TryGetValue(id, glyph), glyph, Nothing)
    End Function

    ' ICON-RENDER 0.12 rule 9: one tone for a menu whose theme the product cannot see. Named in
    ' Theme.vb, the one file that names a colour.
    Public ReadOnly Property Tone As Color
        Get
            Return Theme.MenuIconTone
        End Get
    End Property

    ' Every size Windows asks a menu or a file view for: 16 to 32 for the menu at 100-200 %
    ' scaling, up to 256 for the document type in the large icon views.
    Public ReadOnly Sizes As Integer() = {16, 20, 24, 32, 40, 48, 64, 256}

    Private Const ResourcePrefix As String = "FileDOGUI.menuicons."

    ' Draws one icon at one size: the glyph filling the square, straight (not premultiplied) alpha,
    ' which is what an .ico carries. A new bitmap starts fully transparent.
    Public Function Render(id As String, size As Integer) As Bitmap
        If IsPlated(id) Then Return RenderPlated(id, size)
        Dim bmp As New Bitmap(size, size, PixelFormat.Format32bppArgb)
        Using g = Graphics.FromImage(bmp)
            Glyphs.Draw(g, GlyphRef.Vocabulary(id), New Rectangle(0, 0, size, size), Tone)
        End Using
        Return bmp
    End Function

    ' ---- the plated icon ---------------------------------------------------

    ' The plate's corner radius as a share of its side.
    Public Const PlateCorner As Double = 0.22

    ' The visible plate inside the size x size canvas: a rounded square on whole pixels, so its straight
    ' edges are crisp at every size. The margin that keeps it off the canvas edge is none at 16 and
    ' 20 px (where every pixel is worth more than a margin that Windows' own icon rules add anyway) and
    ' about 3 % above.
    Public Function PlateRect(size As Integer) As Rectangle
        Dim margin = If(size <= 20, 0, Math.Max(1, CInt(Math.Round(size * 0.03))))
        Return New Rectangle(margin, margin, size - 2 * margin, size - 2 * margin)
    End Function

    ' The glyph's 24 grid on the plate: 0.6 of the plate side, within 0.05 (section 10 item E), at the
    ' upper end (0.65) at 16 and 20 px where a smaller glyph stops being legible. Whole pixels, with
    ' the margin to the plate equal on both sides, so the glyph is centred and stays sharp.
    Public Function PlateGlyphSquare(size As Integer) As Rectangle
        Dim plate = PlateRect(size)
        Dim side = plate.Width
        Dim aim = If(size <= 20, 0.65, 0.6)
        Dim g = CInt(Math.Round(side * aim))
        If (side - g) Mod 2 <> 0 Then g = If((g + 1) / side <= 0.65, g + 1, g - 1)
        Dim off = (side - g) \ 2
        Return New Rectangle(plate.X + off, plate.Y + off, g, g)
    End Function

    Private Function RenderPlated(id As String, size As Integer) As Bitmap
        Dim bmp As New Bitmap(size, size, PixelFormat.Format32bppArgb)
        Dim plate = PlateRect(size)
        Dim radius = CSng(plate.Width * PlateCorner)
        Using g = Graphics.FromImage(bmp)
            g.SmoothingMode = SmoothingMode.AntiAlias
            g.PixelOffsetMode = PixelOffsetMode.HighQuality
            Using path = RoundedSquare(plate, radius)
                Using b As New SolidBrush(Theme.AppIconPlate)
                    g.FillPath(b, path)
                End Using
            End Using
            Glyphs.Draw(g, GlyphRef.Vocabulary(PlatedGlyphId(id)), PlateGlyphSquare(size), Theme.AppIconInk)
        End Using
        Return bmp
    End Function

    Private Function RoundedSquare(r As Rectangle, radius As Single) As GraphicsPath
        Dim d = radius * 2.0F
        Dim path As New GraphicsPath()
        path.AddArc(r.X, r.Y, d, d, 180, 90)
        path.AddArc(r.Right - d, r.Y, d, d, 270, 90)
        path.AddArc(r.Right - d, r.Bottom - d, d, d, 0, 90)
        path.AddArc(r.X, r.Bottom - d, d, d, 90, 90)
        path.CloseFigure()
        Return path
    End Function

    ' The .ico of one meaning: 32-bit DIB entries up to 64 px, a PNG entry at 256 (the only size
    ' Windows' icon loader reads as PNG on every version FileDO supports).
    Public Function BuildIco(id As String) As Byte()
        Dim images As New List(Of Byte())()
        For Each size In Sizes
            Using bmp = Render(id, size)
                images.Add(If(size >= 256, PngBytes(bmp), DibBytes(bmp)))
            End Using
        Next
        Using ms As New MemoryStream()
            Using w As New BinaryWriter(ms)
                w.Write(CUShort(0))
                w.Write(CUShort(1))
                w.Write(CUShort(Sizes.Length))
                Dim offset = 6 + 16 * Sizes.Length
                For i = 0 To Sizes.Length - 1
                    Dim s = Sizes(i)
                    w.Write(CByte(If(s >= 256, 0, s)))
                    w.Write(CByte(If(s >= 256, 0, s)))
                    w.Write(CByte(0))
                    w.Write(CByte(0))
                    w.Write(CUShort(1))
                    w.Write(CUShort(32))
                    w.Write(CUInt(images(i).Length))
                    w.Write(CUInt(offset))
                    offset += images(i).Length
                Next
                For Each image In images
                    w.Write(image)
                Next
            End Using
            Return ms.ToArray()
        End Using
    End Function

    ' Writes <id>.ico for every id (mono and plated) into a folder; returns the files written.
    Public Function WriteAll(folder As String) As IList(Of String)
        Directory.CreateDirectory(folder)
        Dim written As New List(Of String)()
        For Each id In AllIds
            Dim path = IO.Path.Combine(folder, id & ".ico")
            File.WriteAllBytes(path, BuildIco(id))
            written.Add(path)
        Next
        Return written
    End Function

    ' The copy of one icon this exe was built with (assets\menu-icons\), or Nothing.
    Public Function EmbeddedIco(id As String) As Byte()
        Using s = GetType(MenuIcons).Assembly.GetManifestResourceStream(ResourcePrefix & id & ".ico")
            If s Is Nothing Then Return Nothing
            Using ms As New MemoryStream()
                s.CopyTo(ms)
                Return ms.ToArray()
            End Using
        End Using
    End Function

    ' The images of an .ico by their pixel size, each decoded to straight ARGB - for the self-test.
    Public Function ReadIco(ico As Byte()) As Dictionary(Of Integer, Bitmap)
        Dim result As New Dictionary(Of Integer, Bitmap)()
        Using r As New BinaryReader(New MemoryStream(ico))
            If r.ReadUInt16() <> 0 OrElse r.ReadUInt16() <> 1 Then Return result
            Dim count = r.ReadUInt16()
            For i = 0 To count - 1
                r.BaseStream.Position = 6 + 16 * i
                Dim w = CInt(r.ReadByte())
                If w = 0 Then w = 256
                r.BaseStream.Position = 6 + 16 * i + 8
                Dim length = CInt(r.ReadUInt32())
                Dim offset = CInt(r.ReadUInt32())
                If offset < 0 OrElse length <= 0 OrElse offset + length > ico.Length Then Continue For
                Dim data(length - 1) As Byte
                Array.Copy(ico, offset, data, 0, length)
                Dim bmp = If(data.Length > 8 AndAlso data(0) = &H89 AndAlso data(1) = &H50,
                             New Bitmap(New Bitmap(New MemoryStream(data))), DibToBitmap(data, w))
                If bmp IsNot Nothing Then result(w) = bmp
            Next
        End Using
        Return result
    End Function

    Private Function PngBytes(bmp As Bitmap) As Byte()
        Using ms As New MemoryStream()
            bmp.Save(ms, ImageFormat.Png)
            Return ms.ToArray()
        End Using
    End Function

    ' BITMAPINFOHEADER, the colour rows bottom-up in BGRA, then the 1-bit AND mask (set where the
    ' pixel is fully transparent), each mask row padded to 32 bits.
    Private Function DibBytes(bmp As Bitmap) As Byte()
        Dim size = bmp.Width
        Dim maskStride = ((size + 31) \ 32) * 4
        Using ms As New MemoryStream()
            Using w As New BinaryWriter(ms)
                w.Write(40)
                w.Write(size)
                w.Write(size * 2)
                w.Write(CUShort(1))
                w.Write(CUShort(32))
                w.Write(0)
                w.Write(size * size * 4 + maskStride * size)
                w.Write(0)
                w.Write(0)
                w.Write(0)
                w.Write(0)
                For y = size - 1 To 0 Step -1
                    For x = 0 To size - 1
                        Dim c = bmp.GetPixel(x, y)
                        w.Write(c.B)
                        w.Write(c.G)
                        w.Write(c.R)
                        w.Write(c.A)
                    Next
                Next
                For y = size - 1 To 0 Step -1
                    Dim row(maskStride - 1) As Byte
                    For x = 0 To size - 1
                        If bmp.GetPixel(x, y).A = 0 Then row(x \ 8) = row(x \ 8) Or CByte(&H80 >> (x Mod 8))
                    Next
                    w.Write(row)
                Next
            End Using
            Return ms.ToArray()
        End Using
    End Function

    Private Function DibToBitmap(data As Byte(), size As Integer) As Bitmap
        If data.Length < 40 + size * size * 4 OrElse BitConverter.ToInt32(data, 0) <> 40 Then Return Nothing
        If BitConverter.ToInt32(data, 4) <> size OrElse BitConverter.ToInt16(data, 14) <> 32 Then Return Nothing
        ' The DIB's rows are bottom-up BGRA, the bitmap's top-down BGRA: copied row by row.
        Dim bmp As New Bitmap(size, size, PixelFormat.Format32bppArgb)
        Dim bits = bmp.LockBits(New Rectangle(0, 0, size, size), ImageLockMode.WriteOnly, PixelFormat.Format32bppArgb)
        Try
            For y = 0 To size - 1
                Runtime.InteropServices.Marshal.Copy(data, 40 + (size - 1 - y) * size * 4, IntPtr.Add(bits.Scan0, y * bits.Stride), size * 4)
            Next
        Finally
            bmp.UnlockBits(bits)
        End Try
        Return bmp
    End Function
End Module
