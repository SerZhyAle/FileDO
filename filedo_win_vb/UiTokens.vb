' The shared size tokens (UI-SIZING 1.1, BUTTONS 2.0 section 3, BOOLEAN-OPTION 2.0 section 2,
' COLLAPSIBLE-GROUP 1.1 section 3, SETTINGS-NAVIGATION 2.0 section 2).
'
' One resolver for every covered component, so a button, a group header and a rail row cannot
' disagree about what "ordinary" is (UI-SIZING rule 2: do not mix a large button with an unscaled
' checkbox, a tiny header or a fixed-size rail). A token is stated in design pixels at 96 DPI and
' already carries the interface scale; the caller turns it into device pixels with Ui.Px, which
' applies the monitor's DPI - so the two scales are composed once and never twice (rule 3).
'
' The product profile (ENGINEERING.md "The GUI shell"): FileDO is a keyboard-and-mouse tool, its
' input profile is the pointer one, chosen here at start and never changed by an input event (rule 1).
' The touch column exists so that a future hybrid choice is a resolver argument, not a rewrite; the
' shell does not expose it. The ordinary pointer target is 32; the smallest supported target is
' 28 at the compact scale (CompactFloor), the floor this product already held and measures.
Friend NotInheritable Class UiTokens
    Private Sub New()
    End Sub

    Friend Enum InputProfile
        Pointer
        Touch
    End Enum

    ' Chosen once, when the shell starts. Not a setting and not changed by any input event.
    Friend Shared ReadOnly Profile As InputProfile = InputProfile.Pointer

    ' The supported interface-scale range: compact (0.875) to huge (2). A value outside it, or not a
    ' number, resolves to the nearest bound or to 1, so no scale produces a zero-size or an
    ' unboundedly shrinking target (rule 4).
    Friend Const MinScale As Double = 0.875
    Friend Const MaxScale As Double = 2.0

    ' The smallest hit target the product supports, in design pixels at 96 DPI (rule 4: "each product
    ' MUST publish its supported smallest target dimensions"). The ordinary pointer target is 32 and
    ' compact (0.875) lands exactly on it.
    Friend Const CompactFloor As Integer = 28

    Private Shared scaleValue As Double = 1.0

    ' The interface scale: text, glyphs, padding, gaps, heading geometry, indentation and targets
    ' scale together (rule 3). Set before the windows are built; a deliberate change is followed by
    ' a remeasure of every covered component at its final width (the 1.1 amendment).
    Friend Shared Property InterfaceScale As Double
        Get
            Return scaleValue
        End Get
        Set(value As Double)
            scaleValue = Clamp(value)
        End Set
    End Property

    Friend Shared Function Clamp(value As Double) As Double
        If Double.IsNaN(value) OrElse Double.IsInfinity(value) Then Return 1.0
        Return Math.Min(MaxScale, Math.Max(MinScale, value))
    End Function

    ' A design-pixel size at the current interface scale (never below 1).
    Friend Shared Function Scaled(designPixels As Integer) As Integer
        Return Scaled(designPixels, scaleValue)
    End Function

    Friend Shared Function Scaled(designPixels As Integer, scale As Double) As Integer
        Return Math.Max(1, CInt(Math.Round(designPixels * Clamp(scale))))
    End Function

    Private Shared Function ByProfile(pointer As Integer, touch As Integer) As Integer
        Return If(Profile = InputProfile.Touch, touch, pointer)
    End Function

    ' ---- BUTTONS 2.0 section 3: the ordinary-size baseline -------------------------------------

    ' Single-line height and hit-target baseline: 32 pointer, 48 touch.
    Friend Shared Function Target() As Integer
        Return Scaled(ByProfile(32, 48))
    End Function

    ' The target at a stated scale, never below the declared smallest target when the scale is in range.
    Friend Shared Function Target(scale As Double) As Integer
        Return Scaled(ByProfile(32, 48), scale)
    End Function

    ' Horizontal padding per side: 12 pointer, 16 touch.
    Friend Shared Function ButtonPadX() As Integer
        Return Scaled(ByProfile(12, 16))
    End Function

    ' The caption glyph tier: 16 pointer, 20 touch.
    Friend Shared Function CaptionGlyph() As Integer
        Return Scaled(ByProfile(16, 20))
    End Function

    ' Glyph-to-caption gap: 8.
    Friend Shared Function GlyphGap() As Integer
        Return Scaled(8)
    End Function

    ' Corner radius 4 (modest, never a pill) and border width 1.
    Friend Shared Function Radius() As Integer
        Return Scaled(4)
    End Function

    ' Button and wrapped-row gap 8; semantic group gap 16.
    Friend Shared Function ButtonGap() As Integer
        Return Scaled(8)
    End Function

    Friend Shared Function GroupGap() As Integer
        Return Scaled(16)
    End Function

    ' ---- COLLAPSIBLE-GROUP 1.1 section 3 -------------------------------------------------------

    ' Header hit height: the ordinary target.
    Friend Shared Function GroupHeaderHeight() As Integer
        Return Target()
    End Function

    ' The state marker's tier is 16; the gap between marker and caption is 8; the body is inset 12
    ' and sibling groups sit 8 apart.
    Friend Shared Function GroupMarker() As Integer
        Return Scaled(16)
    End Function

    Friend Shared Function GroupMarkerGap() As Integer
        Return Scaled(8)
    End Function

    Friend Shared Function GroupBodyInset() As Integer
        Return Scaled(12)
    End Function

    Friend Shared Function GroupSiblingGap() As Integer
        Return Scaled(8)
    End Function

    ' ---- BOOLEAN-OPTION 2.0 section 2 ----------------------------------------------------------

    ' The visible checkbox square: at least 20 pointer, 24 touch.
    Friend Shared Function OptionBox() As Integer
        Return Scaled(ByProfile(20, 24))
    End Function

    ' Checkbox-to-cell gap 8, heading-to-explanation 4, glyph/caption/help 8.
    Friend Shared Function OptionCellGap() As Integer
        Return Scaled(8)
    End Function

    Friend Shared Function OptionExplanationGap() As Integer
        Return Scaled(4)
    End Function

    ' ---- SETTINGS-NAVIGATION 2.0 section 2 -----------------------------------------------------

    ' Ordinary row target 32 pointer, 48 touch; glyph 16 / 20; icon-to-caption gap 8.
    Friend Shared Function NavRow() As Integer
        Return Target()
    End Function

    Friend Shared Function NavGlyph() As Integer
        Return CaptionGlyph()
    End Function
End Class
