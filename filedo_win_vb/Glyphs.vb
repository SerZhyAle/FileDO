Imports System.Drawing.Drawing2D
Imports System.IO
Imports System.Security.Cryptography
Imports System.Text

' The glyphs the shell draws, and the one place that knows where each comes from.
'
' ICON-RENDER rule 1 and section 10 item A, as the owner decided on 2026-09-25 (SP-0016 D1): a
' glyph is the vocabulary's own drawing - Material filled, on the 24 grid - and not a codepoint of
' the platform icon font. The drawings are the catalog's files, vendored under assets/glyphs/ by
' assets/sync-icon-glyphs.ps1 and embedded in this exe beside a PROVENANCE.txt that holds the
' SHA-256 of each one. Nothing here uses a byte it has not hashed: a file whose hash is not its
' line, or a PROVENANCE.txt mapped against another ICON-SET MAJOR, is not drawn at all, and
' Problems() names why (the compatibility law's "a higher MAJOR is refused cleanly").
'
' A meaning the vocabulary has no record for cannot be drawn from it - ICON-SET rule 5 puts the
' record first. Such a glyph names the id FileDO proposed for it (the catalog's
' PROPOSAL-2026-09-23-filedo-meanings.md) and draws its pre-contract Segoe codepoint meanwhile,
' which is the dated exception of SP-0016 T1; assets/sync-icon-glyphs.ps1 -Check reports the day
' the record lands.

' Which glyph a control shows: a vocabulary meaning, or a meaning still waiting for its record.
Public NotInheritable Class GlyphRef

    ' The vocabulary id drawn, or "" while the meaning waits for one.
    Public ReadOnly Id As String
    ' The id FileDO proposed for a waiting meaning.
    Public ReadOnly Pending As String
    ' The codepoint of Segoe Fluent Icons (Segoe MDL2 Assets on Windows 10) drawn while the meaning
    ' waits, and that font's own name for it - ICON-EXTERNAL rule 5, the source is on record.
    Public ReadOnly Interim As Char
    Public ReadOnly FontName As String
    ' Text any UI font has, for when neither can be drawn. A bullet, unless the glyph carries a state
    ' a bullet would lose (the rail's group chevrons).
    Public ReadOnly Fallback As String

    Private Sub New(id As String, pending As String, interim As Char, fontName As String, fallback As String)
        Me.Id = id
        Me.Pending = pending
        Me.Interim = interim
        Me.FontName = fontName
        Me.Fallback = If(fallback, ChrW(&H2022))
    End Sub

    Public Shared Function Vocabulary(id As String, Optional fallback As String = Nothing) As GlyphRef
        Return New GlyphRef(id, "", ChrW(0), "", fallback)
    End Function

    Public Shared Function Waiting(pending As String, codepoint As Integer, fontName As String) As GlyphRef
        Return New GlyphRef("", pending, ChrW(codepoint), fontName, Nothing)
    End Function

    Public ReadOnly Property IsVocabulary As Boolean
        Get
            Return Id <> ""
        End Get
    End Property

    ' The meaning shown, whichever way it is drawn: what two controls must not share (ICON-SET 1).
    Public ReadOnly Property Meaning As String
        Get
            Return If(IsVocabulary, Id, Pending)
        End Get
    End Property

    Public Overrides Function ToString() As String
        If IsVocabulary Then Return Id
        Return Pending & " (waiting; U+" & AscW(Interim).ToString("X4") & " " & FontName & ")"
    End Function

End Class

Public Module Glyphs

    ' The ICON-SET MAJOR this build's mapping was made against. A PROVENANCE.txt from another MAJOR
    ' means a shape or a name changed, which a person re-maps before anything is drawn from it.
    Public Const MappedIconSetMajor As Integer = 0

    Private Const ResourcePrefix As String = "FileDOGUI.glyphs."
    Private Const ProvenanceName As String = "PROVENANCE.txt"

    Private loaded As Boolean = False
    Private ReadOnly shapes As New Dictionary(Of String, List(Of SvgPath.Shape))(StringComparer.Ordinal)
    Private ReadOnly verifiedTexts As New Dictionary(Of String, String)(StringComparer.Ordinal)
    Private ReadOnly problemList As New List(Of String)()
    Private catalogVersionsValue As String = ""
    Private recordedFiles As Integer = 0

    ' ---- the vendored set --------------------------------------------------

    Private Sub EnsureLoaded()
        If loaded Then Return
        loaded = True
        Try
            Load()
        Catch ex As Exception
            ShellLog.Write("glyphs: reading the vendored set", ex)
            problemList.Add("the vendored glyphs could not be read: " & ex.GetType().Name)
            shapes.Clear()
            verifiedTexts.Clear()
        End Try
    End Sub

    Private Sub Load()
        Dim asm = GetType(Glyphs).Assembly
        Dim provenance = ReadResource(asm, ResourcePrefix & ProvenanceName)
        If provenance Is Nothing Then
            problemList.Add("PROVENANCE.txt is not embedded")
            Return
        End If

        Dim lines = Encoding.UTF8.GetString(provenance).Split(New String() {vbCrLf, vbLf}, StringSplitOptions.None)
        Dim recorded As New Dictionary(Of String, String)(StringComparer.Ordinal)
        Dim major As Integer = -1
        For Each raw In lines
            Dim line = raw.Trim()
            If line.StartsWith("Catalog versions:", StringComparison.Ordinal) Then
                catalogVersionsValue = line.Substring("Catalog versions:".Length).Trim()
                Dim m = System.Text.RegularExpressions.Regex.Match(catalogVersionsValue, "ICON-SET (\d+)\.")
                If m.Success Then major = Integer.Parse(m.Groups(1).Value)
                Continue For
            End If
            Dim parts = line.Split(New Char() {" "c}, StringSplitOptions.RemoveEmptyEntries)
            If parts.Length = 2 AndAlso (parts(0).EndsWith(".svg", StringComparison.Ordinal) OrElse
                                         parts(0).EndsWith(".json", StringComparison.Ordinal)) Then
                recorded(parts(0)) = parts(1).ToLowerInvariant()
            End If
        Next
        recordedFiles = recorded.Count

        If major <> MappedIconSetMajor Then
            problemList.Add("PROVENANCE.txt records ICON-SET " & If(major < 0, "(no version)", major.ToString() & ".x") &
                            ", this build maps ICON-SET " & MappedIconSetMajor.ToString() & ".x - nothing is drawn from it")
            Return
        End If

        For Each name In asm.GetManifestResourceNames()
            If Not name.StartsWith(ResourcePrefix, StringComparison.Ordinal) Then Continue For
            Dim file = name.Substring(ResourcePrefix.Length)
            If file <> ProvenanceName AndAlso Not recorded.ContainsKey(file) Then
                problemList.Add(file & " is embedded but has no line in PROVENANCE.txt")
            End If
        Next

        For Each kv In recorded
            Dim bytes = ReadResource(asm, ResourcePrefix & kv.Key)
            If bytes Is Nothing Then
                problemList.Add(kv.Key & " is in PROVENANCE.txt but not embedded")
                Continue For
            End If
            Dim hash = Sha256Hex(bytes)
            If hash <> kv.Value Then
                problemList.Add(kv.Key & " does not match PROVENANCE.txt (" & hash & ")")
                Continue For
            End If
            Dim text = Encoding.UTF8.GetString(bytes)
            If kv.Key.EndsWith(".svg", StringComparison.Ordinal) Then
                Dim id = kv.Key.Substring(0, kv.Key.Length - ".svg".Length)
                Try
                    Dim parts = SvgPath.ReadShapes(text)
                    If parts.Count = 0 Then
                        problemList.Add(kv.Key & " has no path to draw")
                    Else
                        shapes(id) = parts
                    End If
                Catch ex As Exception
                    ShellLog.Write("glyphs: reading " & kv.Key, ex)
                    problemList.Add(kv.Key & " could not be read: " & ex.GetType().Name)
                End Try
            Else
                verifiedTexts(kv.Key) = text
            End If
        Next
    End Sub

    Private Function ReadResource(asm As Reflection.Assembly, name As String) As Byte()
        Using s = asm.GetManifestResourceStream(name)
            If s Is Nothing Then Return Nothing
            Using ms As New MemoryStream()
                s.CopyTo(ms)
                Return ms.ToArray()
            End Using
        End Using
    End Function

    Private Function Sha256Hex(bytes As Byte()) As String
        Using sha = SHA256.Create()
            Dim sb As New StringBuilder()
            For Each b In sha.ComputeHash(bytes)
                sb.Append(b.ToString("x2"))
            Next
            Return sb.ToString()
        End Using
    End Function

    ' Why a vendored file is not being drawn - empty when every one verified. The self-test fails on
    ' any line here; the diagnostics report carries the count.
    Public Function Problems() As IList(Of String)
        EnsureLoaded()
        Return problemList.AsReadOnly()
    End Function

    ' "ICON-SET 0.13; ICON-RENDER 0.11; ICON-EXTERNAL 0.9", as PROVENANCE.txt records it.
    Public Function CatalogVersions() As String
        EnsureLoaded()
        Return catalogVersionsValue
    End Function

    Public Function RecordedFileCount() As Integer
        EnsureLoaded()
        Return recordedFiles
    End Function

    ' The ids whose drawing verified and parsed.
    Public Function DrawableIds() As IEnumerable(Of String)
        EnsureLoaded()
        Return shapes.Keys.ToList()
    End Function

    Public Function IsDrawable(id As String) As Boolean
        EnsureLoaded()
        Return id IsNot Nothing AndAlso shapes.ContainsKey(id)
    End Function

    ' A vendored data file (palette.json), only once its hash verified; Nothing otherwise.
    Public Function VerifiedData(name As String) As String
        EnsureLoaded()
        Dim text As String = Nothing
        Return If(verifiedTexts.TryGetValue(name, text), text, Nothing)
    End Function

    ' The inked box of a drawable glyph, in grid units - for the self-test.
    Public Function InkBounds(id As String) As RectangleF
        EnsureLoaded()
        Dim parts As List(Of SvgPath.Shape) = Nothing
        If Not shapes.TryGetValue(id, parts) Then Return RectangleF.Empty
        Dim box = parts(0).InkBounds()
        For i = 1 To parts.Count - 1
            box = RectangleF.Union(box, parts(i).InkBounds())
        Next
        Return box
    End Function

    ' ---- drawing -----------------------------------------------------------

    ' Draws a glyph filling the square it is given (the square is the glyph's tier, ICON-RENDER rule
    ' 5), in one colour - the role the caller resolved from the palette in force (rule 2).
    Public Sub Draw(g As Graphics, glyph As GlyphRef, square As Rectangle, colour As Color)
        If glyph Is Nothing OrElse square.Width <= 0 OrElse square.Height <= 0 Then Return
        EnsureLoaded()
        Dim parts As List(Of SvgPath.Shape) = Nothing
        If glyph.IsVocabulary AndAlso shapes.TryGetValue(glyph.Id, parts) Then
            DrawShapes(g, parts, square, colour)
        ElseIf Not glyph.IsVocabulary AndAlso Theme.HasGlyphFont() Then
            TextRenderer.DrawText(g, glyph.Interim.ToString(), InterimFont(square.Height), square, colour,
                                  TextFormatFlags.HorizontalCenter Or TextFormatFlags.VerticalCenter Or
                                  TextFormatFlags.NoPrefix Or TextFormatFlags.NoPadding)
        Else
            TextRenderer.DrawText(g, glyph.Fallback, FallbackFont(square.Height), square, colour,
                                  TextFormatFlags.HorizontalCenter Or TextFormatFlags.VerticalCenter Or
                                  TextFormatFlags.NoPrefix Or TextFormatFlags.NoPadding)
        End If
    End Sub

    Private Sub DrawShapes(g As Graphics, parts As List(Of SvgPath.Shape), square As Rectangle, colour As Color)
        Dim oldSmoothing = g.SmoothingMode
        Dim oldOffset = g.PixelOffsetMode
        g.SmoothingMode = SmoothingMode.AntiAlias
        g.PixelOffsetMode = PixelOffsetMode.HighQuality
        Try
            Dim scale = square.Width / 24.0F
            Using m As New Matrix()
                m.Translate(square.X, square.Y)
                m.Scale(scale, scale)
                For Each part In parts
                    Dim ink = If(part.Opacity >= 1.0F, colour, Theme.Faded(colour, part.Opacity))
                    Using path = DirectCast(part.Path.Clone(), GraphicsPath)
                        path.Transform(m)
                        If part.Fills Then
                            Using b As New SolidBrush(ink)
                                g.FillPath(b, path)
                            End Using
                        End If
                        If part.StrokeWidth > 0 Then
                            Using p As New Pen(ink, part.StrokeWidth * scale)
                                g.DrawPath(p, path)
                            End Using
                        End If
                    End Using
                Next
            End Using
        Finally
            g.SmoothingMode = oldSmoothing
            g.PixelOffsetMode = oldOffset
        End Try
    End Sub

    ' The fonts a stand-in is drawn in, made once per pixel size rather than once per paint: a paint
    ' runs on every hover, and a font made per paint is a GDI object leaked each time.
    Private ReadOnly interimFonts As New Dictionary(Of Integer, Font)()
    Private ReadOnly fallbackFonts As New Dictionary(Of Integer, Font)()

    ' A Segoe glyph fills its em box where a Material one keeps a unit of margin on the 24 grid, so
    ' a stand-in is drawn at 0.8 of the square - 16 px in the rail's 20 px tier, the size the rail
    ' drew it at before the vocabulary's drawings arrived - to sit at the same visual size.
    Private Function InterimFont(squarePx As Integer) As Font
        Dim px = Math.Max(6, CInt(Math.Round(squarePx * 0.8)))
        Dim f As Font = Nothing
        If Not interimFonts.TryGetValue(px, f) Then
            f = New Font(Theme.GlyphFamily(), px, FontStyle.Regular, GraphicsUnit.Pixel)
            interimFonts(px) = f
        End If
        Return f
    End Function

    Private Function FallbackFont(squarePx As Integer) As Font
        Dim px = Math.Max(6, CInt(Math.Round(squarePx * 0.7)))
        Dim f As Font = Nothing
        If Not fallbackFonts.TryGetValue(px, f) Then
            f = New Font(Theme.UiFamily(), px, FontStyle.Regular, GraphicsUnit.Pixel)
            fallbackFonts(px) = f
        End If
        Return f
    End Function

End Module

' One glyph on its own: the verdict beside its badge on the job page and beside its line on the
' Command page. It is decoration - the word next to it carries the meaning, so a screen reader meets
' the verdict once rather than a glyph and then the word (APP-BEHAVIOUR rule 9, ICON-RENDER rule 8
' "or is marked decorative") - and it paints in its ForeColor, which the page sets from the role.
Public Class GlyphBox
    Inherits Control

    Private glyphValue As GlyphRef = Nothing
    Private tierValue As Integer = 24

    Public Sub New()
        SetStyle(ControlStyles.AllPaintingInWmPaint Or
                 ControlStyles.OptimizedDoubleBuffer Or
                 ControlStyles.ResizeRedraw Or
                 ControlStyles.UserPaint Or
                 ControlStyles.SupportsTransparentBackColor, True)
        SetStyle(ControlStyles.Selectable, False)
        TabStop = False
        AccessibleRole = AccessibleRole.None
        AutoSize = True
        Size = GetPreferredSize(Size.Empty)
    End Sub

    Public Property Glyph As GlyphRef
        Get
            Return glyphValue
        End Get
        Set(value As GlyphRef)
            glyphValue = value
            Invalidate()
        End Set
    End Property

    ' The size tier in design pixels (ICON-RENDER rule 5 and section 10 item E: 16, 20, 24, 32, 40, 48).
    Public Property Tier As Integer
        Get
            Return tierValue
        End Get
        Set(value As Integer)
            tierValue = value
            Size = GetPreferredSize(Size.Empty)
            Invalidate()
        End Set
    End Property

    Public Overrides Function GetPreferredSize(proposedSize As Size) As Size
        Dim px = Ui.Px(Me, tierValue)
        Return New Size(px, px)
    End Function

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        MyBase.OnPaint(e)
        If glyphValue Is Nothing Then Return
        Dim px = Math.Min(Ui.Px(Me, tierValue), Math.Min(ClientSize.Width, ClientSize.Height))
        Dim square As New Rectangle((ClientSize.Width - px) \ 2, (ClientSize.Height - px) \ 2, px, px)
        Glyphs.Draw(e.Graphics, glyphValue, square, ForeColor)
    End Sub

End Class
