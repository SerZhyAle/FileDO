Imports System.Drawing.Drawing2D
Imports System.Globalization
Imports System.Text.RegularExpressions
Imports System.Xml.Linq

' The part of SVG the iconography catalog's glyph files use, read into GDI+ paths on the catalog's
' 24 x 24 grid. System.Drawing has no SVG loader, so a product that draws the catalog's own files
' reads their path data itself; this is the same reader Fast Media Sorter for Windows ships, with
' one correction (arc flags, below).
'
' A partial reader fails silently - it draws another picture rather than throwing - which is why it
' reads the whole subset the catalog uses and not only what today's glyphs need: nested <g
' transform> (translate, scale, rotate, matrix), fill (currentColor or none), fill-rule, stroke with
' stroke-width, and opacity / fill-opacity. Anything painted is painted in the ONE colour of the
' control (ICON-RENDER rule 2): a mono glyph has no second colour.
Module SvgPath

    ' One drawable part of a glyph: its outline in grid units, every enclosing transform applied,
    ' and the paint the file gave it.
    Public NotInheritable Class Shape
        Implements IDisposable

        Public ReadOnly Path As GraphicsPath
        Public ReadOnly Fills As Boolean
        Public ReadOnly StrokeWidth As Single    ' 0 when the part is not stroked
        Public ReadOnly Opacity As Single

        Friend Sub New(path As GraphicsPath, fills As Boolean, strokeWidth As Single, opacity As Single)
            Me.Path = path
            Me.Fills = fills
            Me.StrokeWidth = strokeWidth
            Me.Opacity = opacity
        End Sub

        ' The inked box in grid units: the outline's bounds, widened by half the stroke.
        Public Function InkBounds() As RectangleF
            Dim b = Path.GetBounds()
            If StrokeWidth > 0 Then b.Inflate(StrokeWidth / 2.0F, StrokeWidth / 2.0F)
            Return b
        End Function

        Public Sub Dispose() Implements IDisposable.Dispose
            Path.Dispose()
        End Sub
    End Class

    Private ReadOnly tokenPattern As New Regex(
        "[AaCcHhLlMmQqSsTtVvZz]|[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?", RegexOptions.Compiled)
    Private ReadOnly transformPattern As New Regex(
        "(matrix|translate|scale|rotate)\s*\(([^)]*)\)", RegexOptions.Compiled Or RegexOptions.IgnoreCase)
    Private ReadOnly numberPattern As New Regex(
        "[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?", RegexOptions.Compiled)

    ' Every painted part of one glyph file, in grid units.
    Public Function ReadShapes(svg As String) As List(Of Shape)
        Dim result As New List(Of Shape)()
        Dim doc = XDocument.Parse(svg)
        Using identity As New Matrix()
            Walk(doc.Root, identity, 1.0F, result)
        End Using
        Return result
    End Function

    Private Sub Walk(element As XElement, parent As Matrix, parentOpacity As Single, result As List(Of Shape))
        Using m = parent.Clone()
            Dim transform = Attr(element, "transform")
            If transform.Length > 0 Then ApplyTransform(m, transform)
            Dim opacity = parentOpacity * ReadFraction(Attr(element, "opacity"))

            If element.Name.LocalName = "path" Then
                Dim d = Attr(element, "d")
                If d.Length > 0 Then
                    Dim fill = Inherited(element, "fill")
                    Dim stroke = Inherited(element, "stroke")
                    Dim fills = Not String.Equals(fill, "none", StringComparison.OrdinalIgnoreCase)
                    Dim strokeWidth As Single = 0
                    If stroke.Length > 0 AndAlso Not String.Equals(stroke, "none", StringComparison.OrdinalIgnoreCase) Then
                        Dim w = Inherited(element, "stroke-width")
                        strokeWidth = If(w.Length > 0, ParseNumber(w), 1.0F)
                    End If
                    Dim fillOpacity = ReadFraction(Attr(element, "fill-opacity"))
                    Dim path = Parse(d)
                    path.FillMode = If(String.Equals(Inherited(element, "fill-rule"), "evenodd", StringComparison.OrdinalIgnoreCase),
                                       FillMode.Alternate, FillMode.Winding)
                    path.Transform(m)
                    ' A transform that scales also scales the stroke.
                    Dim scale = CSng(Math.Sqrt(Math.Abs(m.Elements(0) * m.Elements(3) - m.Elements(1) * m.Elements(2))))
                    result.Add(New Shape(path, fills, strokeWidth * scale, opacity * If(fills, fillOpacity, 1.0F)))
                End If
            End If

            For Each child In element.Elements()
                Walk(child, m, opacity, result)
            Next
        End Using
    End Sub

    Private Sub ApplyTransform(m As Matrix, transform As String)
        ' SVG applies the list right to left to a point, which is left to right prepended.
        For Each op As Match In transformPattern.Matches(transform)
            Dim args As New List(Of Single)()
            For Each n As Match In numberPattern.Matches(op.Groups(2).Value)
                args.Add(ParseNumber(n.Value))
            Next
            Select Case op.Groups(1).Value.ToLowerInvariant()
                Case "translate"
                    If args.Count >= 1 Then m.Translate(args(0), If(args.Count >= 2, args(1), 0.0F), MatrixOrder.Prepend)
                Case "scale"
                    If args.Count >= 1 Then m.Scale(args(0), If(args.Count >= 2, args(1), args(0)), MatrixOrder.Prepend)
                Case "rotate"
                    If args.Count >= 3 Then
                        m.RotateAt(args(0), New PointF(args(1), args(2)), MatrixOrder.Prepend)
                    ElseIf args.Count >= 1 Then
                        m.Rotate(args(0), MatrixOrder.Prepend)
                    End If
                Case "matrix"
                    If args.Count >= 6 Then
                        Using op6 As New Matrix(args(0), args(1), args(2), args(3), args(4), args(5))
                            m.Multiply(op6, MatrixOrder.Prepend)
                        End Using
                    End If
            End Select
        Next
    End Sub

    ' A presentation attribute, looked up the tree as SVG inherits it.
    Private Function Inherited(element As XElement, name As String) As String
        Dim e = element
        While e IsNot Nothing
            Dim v = Attr(e, name)
            If v.Length > 0 Then Return v
            e = e.Parent
        End While
        Return ""
    End Function

    Private Function Attr(element As XElement, name As String) As String
        Dim a = element.Attribute(name)
        Return If(a Is Nothing, "", a.Value.Trim())
    End Function

    Private Function ReadFraction(value As String) As Single
        If value.Length = 0 Then Return 1.0F
        Return Math.Max(0.0F, Math.Min(1.0F, ParseNumber(value)))
    End Function

    Private Function ParseNumber(value As String) As Single
        Return Single.Parse(value, NumberStyles.Float, CultureInfo.InvariantCulture)
    End Function

    ' ---- path data -------------------------------------------------------

    ' One "d" attribute as a GraphicsPath in the file's own units.
    Public Function Parse(pathData As String) As GraphicsPath
        Dim path As New GraphicsPath()
        If String.IsNullOrWhiteSpace(pathData) Then Return path

        Dim tokens As New List(Of String)()
        For Each match As Match In tokenPattern.Matches(pathData)
            tokens.Add(match.Value)
        Next

        Dim reader As New PathReader(tokens)
        Dim current = PointF.Empty
        Dim figureStart = PointF.Empty
        Dim lastCubicControl = PointF.Empty
        Dim lastQuadraticControl = PointF.Empty
        Dim previousCommand As Char = ChrW(0)
        Dim command As Char = ChrW(0)

        While reader.HasMore
            Dim nextCommand As Char
            If reader.TryReadCommand(nextCommand) Then
                command = nextCommand
            ElseIf command = ChrW(0) Then
                Throw New FormatException("SVG path starts without a command.")
            End If

            Dim relative = Char.IsLower(command)
            Select Case Char.ToUpperInvariant(command)
                Case "M"c
                    Dim first = True
                    Do While reader.HasNumber
                        Dim point = ReadPoint(reader, relative, current)
                        If first Then
                            path.StartFigure()
                            current = point
                            figureStart = point
                            first = False
                        Else
                            path.AddLine(current, point)
                            current = point
                        End If
                    Loop
                Case "L"c
                    Do While reader.HasNumber
                        Dim point = ReadPoint(reader, relative, current)
                        path.AddLine(current, point)
                        current = point
                    Loop
                Case "H"c
                    Do While reader.HasNumber
                        Dim x = reader.ReadNumber()
                        If relative Then x += current.X
                        Dim point As New PointF(x, current.Y)
                        path.AddLine(current, point)
                        current = point
                    Loop
                Case "V"c
                    Do While reader.HasNumber
                        Dim y = reader.ReadNumber()
                        If relative Then y += current.Y
                        Dim point As New PointF(current.X, y)
                        path.AddLine(current, point)
                        current = point
                    Loop
                Case "C"c
                    Do While reader.HasNumber
                        Dim c1 = ReadPoint(reader, relative, current)
                        Dim c2 = ReadPoint(reader, relative, current)
                        Dim point = ReadPoint(reader, relative, current)
                        path.AddBezier(current, c1, c2, point)
                        current = point
                        lastCubicControl = c2
                    Loop
                Case "S"c
                    Do While reader.HasNumber
                        Dim prev = Char.ToUpperInvariant(previousCommand)
                        Dim c1 = If(prev = "C"c OrElse prev = "S"c, Reflect(lastCubicControl, current), current)
                        Dim c2 = ReadPoint(reader, relative, current)
                        Dim point = ReadPoint(reader, relative, current)
                        path.AddBezier(current, c1, c2, point)
                        current = point
                        lastCubicControl = c2
                        previousCommand = "S"c
                    Loop
                Case "Q"c
                    Do While reader.HasNumber
                        Dim control = ReadPoint(reader, relative, current)
                        Dim point = ReadPoint(reader, relative, current)
                        AddQuadratic(path, current, control, point)
                        current = point
                        lastQuadraticControl = control
                    Loop
                Case "T"c
                    Do While reader.HasNumber
                        Dim prev = Char.ToUpperInvariant(previousCommand)
                        Dim control = If(prev = "Q"c OrElse prev = "T"c, Reflect(lastQuadraticControl, current), current)
                        Dim point = ReadPoint(reader, relative, current)
                        AddQuadratic(path, current, control, point)
                        current = point
                        lastQuadraticControl = control
                        previousCommand = "T"c
                    Loop
                Case "A"c
                    Do While reader.HasNumber
                        Dim rx = Math.Abs(reader.ReadNumber())
                        Dim ry = Math.Abs(reader.ReadNumber())
                        Dim rotation = reader.ReadNumber()
                        Dim largeArc = reader.ReadFlag()
                        Dim sweep = reader.ReadFlag()
                        Dim point = ReadPoint(reader, relative, current)
                        AddArc(path, current, rx, ry, rotation, largeArc, sweep, point)
                        current = point
                    Loop
                Case "Z"c
                    path.CloseFigure()
                    current = figureStart
                    ' Z takes no numbers; one here would be read as Z again, for ever.
                    If reader.HasNumber Then Throw New FormatException("SVG path has a number after Z.")
                Case Else
                    Throw New FormatException("Unsupported SVG path command '" & command & "'.")
            End Select
            previousCommand = command
        End While
        Return path
    End Function

    Private Function ReadPoint(reader As PathReader, relative As Boolean, origin As PointF) As PointF
        Dim x = reader.ReadNumber()
        Dim y = reader.ReadNumber()
        If relative Then Return New PointF(origin.X + x, origin.Y + y)
        Return New PointF(x, y)
    End Function

    Private Function Reflect(control As PointF, around As PointF) As PointF
        Return New PointF(2.0F * around.X - control.X, 2.0F * around.Y - control.Y)
    End Function

    Private Sub AddQuadratic(path As GraphicsPath, startPoint As PointF, control As PointF, endPoint As PointF)
        Dim c1 As New PointF(startPoint.X + (2.0F / 3.0F) * (control.X - startPoint.X),
                             startPoint.Y + (2.0F / 3.0F) * (control.Y - startPoint.Y))
        Dim c2 As New PointF(endPoint.X + (2.0F / 3.0F) * (control.X - endPoint.X),
                             endPoint.Y + (2.0F / 3.0F) * (control.Y - endPoint.Y))
        path.AddBezier(startPoint, c1, c2, endPoint)
    End Sub

    ' The endpoint-to-centre conversion of the SVG 1.1 implementation notes, drawn as cubic segments
    ' of at most a quarter turn each, so the result owes nothing to any SVG runtime.
    Private Sub AddArc(path As GraphicsPath, startPoint As PointF, rx As Single, ry As Single,
                       rotationDegrees As Single, largeArc As Boolean, sweep As Boolean, endPoint As PointF)
        If startPoint = endPoint Then Return
        If rx = 0.0F OrElse ry = 0.0F Then
            path.AddLine(startPoint, endPoint)
            Return
        End If

        Dim phi = rotationDegrees * Math.PI / 180.0
        Dim cosPhi = Math.Cos(phi)
        Dim sinPhi = Math.Sin(phi)
        Dim dx = (startPoint.X - endPoint.X) / 2.0
        Dim dy = (startPoint.Y - endPoint.Y) / 2.0
        Dim x1p = cosPhi * dx + sinPhi * dy
        Dim y1p = -sinPhi * dx + cosPhi * dy
        Dim rxd As Double = rx
        Dim ryd As Double = ry
        Dim lambda = x1p * x1p / (rxd * rxd) + y1p * y1p / (ryd * ryd)
        If lambda > 1.0 Then
            Dim grow = Math.Sqrt(lambda)
            rxd *= grow
            ryd *= grow
        End If

        Dim numerator = rxd * rxd * ryd * ryd - rxd * rxd * y1p * y1p - ryd * ryd * x1p * x1p
        Dim denominator = rxd * rxd * y1p * y1p + ryd * ryd * x1p * x1p
        Dim factor = If(denominator = 0.0, 0.0, Math.Sqrt(Math.Max(0.0, numerator / denominator)))
        If largeArc = sweep Then factor = -factor
        Dim cxp = factor * (rxd * y1p / ryd)
        Dim cyp = factor * (-ryd * x1p / rxd)
        Dim cx = cosPhi * cxp - sinPhi * cyp + (startPoint.X + endPoint.X) / 2.0
        Dim cy = sinPhi * cxp + cosPhi * cyp + (startPoint.Y + endPoint.Y) / 2.0
        Dim theta1 = VectorAngle(1.0, 0.0, (x1p - cxp) / rxd, (y1p - cyp) / ryd)
        Dim delta = VectorAngle((x1p - cxp) / rxd, (y1p - cyp) / ryd, (-x1p - cxp) / rxd, (-y1p - cyp) / ryd)
        If Not sweep AndAlso delta > 0.0 Then delta -= 2.0 * Math.PI
        If sweep AndAlso delta < 0.0 Then delta += 2.0 * Math.PI
        Dim segments = Math.Max(1, CInt(Math.Ceiling(Math.Abs(delta) / (Math.PI / 2.0))))
        Dim stepSize = delta / segments
        Dim fromAngle = theta1
        For i = 0 To segments - 1
            Dim toAngle = fromAngle + stepSize
            AddArcSegment(path, cx, cy, rxd, ryd, cosPhi, sinPhi, fromAngle, toAngle)
            fromAngle = toAngle
        Next
    End Sub

    Private Sub AddArcSegment(path As GraphicsPath, cx As Double, cy As Double, rx As Double, ry As Double,
                              cosPhi As Double, sinPhi As Double, fromAngle As Double, toAngle As Double)
        Dim alpha = 4.0 / 3.0 * Math.Tan((toAngle - fromAngle) / 4.0)
        Dim p0 = ArcPoint(cx, cy, rx, ry, cosPhi, sinPhi, fromAngle)
        Dim p3 = ArcPoint(cx, cy, rx, ry, cosPhi, sinPhi, toAngle)
        Dim d0 = ArcDerivative(rx, ry, cosPhi, sinPhi, fromAngle)
        Dim d3 = ArcDerivative(rx, ry, cosPhi, sinPhi, toAngle)
        Dim p1 As New PointF(p0.X + CSng(alpha) * d0.X, p0.Y + CSng(alpha) * d0.Y)
        Dim p2 As New PointF(p3.X - CSng(alpha) * d3.X, p3.Y - CSng(alpha) * d3.Y)
        path.AddBezier(p0, p1, p2, p3)
    End Sub

    Private Function ArcPoint(cx As Double, cy As Double, rx As Double, ry As Double,
                              cosPhi As Double, sinPhi As Double, angle As Double) As PointF
        Dim x = rx * Math.Cos(angle)
        Dim y = ry * Math.Sin(angle)
        Return New PointF(CSng(cx + cosPhi * x - sinPhi * y), CSng(cy + sinPhi * x + cosPhi * y))
    End Function

    Private Function ArcDerivative(rx As Double, ry As Double, cosPhi As Double, sinPhi As Double,
                                   angle As Double) As PointF
        Dim x = -rx * Math.Sin(angle)
        Dim y = ry * Math.Cos(angle)
        Return New PointF(CSng(cosPhi * x - sinPhi * y), CSng(sinPhi * x + cosPhi * y))
    End Function

    Private Function VectorAngle(ux As Double, uy As Double, vx As Double, vy As Double) As Double
        Return Math.Atan2(ux * vy - uy * vx, ux * vx + uy * vy)
    End Function

    Private NotInheritable Class PathReader
        Private ReadOnly tokens As List(Of String)
        Private index As Integer

        Public Sub New(values As List(Of String))
            tokens = values
        End Sub

        Public ReadOnly Property HasMore As Boolean
            Get
                Return index < tokens.Count
            End Get
        End Property

        Public ReadOnly Property HasNumber As Boolean
            Get
                Return HasMore AndAlso Not IsCommand(tokens(index))
            End Get
        End Property

        Public Function TryReadCommand(ByRef value As Char) As Boolean
            If Not HasMore OrElse Not IsCommand(tokens(index)) Then Return False
            value = tokens(index)(0)
            index += 1
            Return True
        End Function

        Public Function ReadNumber() As Single
            If Not HasNumber Then Throw New FormatException("SVG path has too few parameters.")
            Dim token = tokens(index)
            index += 1
            Return Single.Parse(token, NumberStyles.Float, CultureInfo.InvariantCulture)
        End Function

        ' An arc flag is one character, and SVG lets it touch what follows: "a2 2 0 012-2" is the
        ' flags 0 and 1 and then the point (2, -2). The number pattern reads "012" as one number, so
        ' a flag takes the first digit of the token and leaves the rest of it to be read next.
        Public Function ReadFlag() As Boolean
            If Not HasNumber Then Throw New FormatException("SVG arc has too few flags.")
            Dim token = tokens(index)
            Dim c = token(0)
            If c <> "0"c AndAlso c <> "1"c Then Throw New FormatException("SVG arc flag is not 0 or 1: " & token)
            If token.Length = 1 Then
                index += 1
            Else
                tokens(index) = token.Substring(1)
            End If
            Return c = "1"c
        End Function

        Private Shared Function IsCommand(value As String) As Boolean
            Return value.Length = 1 AndAlso Char.IsLetter(value(0))
        End Function
    End Class

End Module
