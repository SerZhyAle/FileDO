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
Imports System.Drawing.Imaging
Imports System.IO

Public Module MenuIcons

    ' The meanings the Explorer surfaces show: the ten menu entries share five, the document type
    ' is the sixth. Which entry takes which is the writers' table (fdsecMenuItems, FileDO.wxs,
    ' FileDOShell.cpp), held equal by their parity tests.
    Public ReadOnly Ids As String() = {
        "action.secure", "action.unsecure", "action.wipe", "action.verify", "app.info", "content.secret-file"
    }

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
        Dim bmp As New Bitmap(size, size, PixelFormat.Format32bppArgb)
        Using g = Graphics.FromImage(bmp)
            Glyphs.Draw(g, GlyphRef.Vocabulary(id), New Rectangle(0, 0, size, size), Tone)
        End Using
        Return bmp
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

    ' Writes <id>.ico for every id into a folder; returns the files written.
    Public Function WriteAll(folder As String) As IList(Of String)
        Directory.CreateDirectory(folder)
        Dim written As New List(Of String)()
        For Each id In Ids
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
