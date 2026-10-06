' Rebind presentation without rebuilding a running page or changing its editor values.
Imports System.Reflection
Imports System.Text.RegularExpressions

Friend Module LiveLanguage
    Private ReadOnly fields As BindingFlags = BindingFlags.Instance Or BindingFlags.NonPublic Or BindingFlags.Public Or BindingFlags.DeclaredOnly

    Friend Sub Apply(before As String, after As String)
        For Each window As Form In Application.OpenForms.Cast(Of Form)().ToArray()
            RebindWindow(window, before, after)
        Next
    End Sub

    ' The self-test's seam: one window rebound without having to be shown, because a gate run must
    ' not flash a window on the user's screen to prove the mechanism (SP-0150).
    Friend Sub RebindWindow(window As Form, before As String, after As String)
        Dim oldText = Localization.GetDict(before)
        Dim newText = Localization.GetDict(after)
        Dim byText As New Dictionary(Of String, String)(StringComparer.Ordinal)
        For Each pair In oldText
            Dim source = Localization.Multiline(pair.Value)
            If Not byText.ContainsKey(source) Then byText(source) = pair.Key
        Next
        Ui.SuspendTree(window)
        Try
            Rebind(window, byText, newText)
            window.RightToLeft = If(Localization.IsRightToLeft(after), RightToLeft.Yes, RightToLeft.No)
            window.RightToLeftLayout = Localization.IsRightToLeft(after)
        Finally
            Ui.ResumeTree(window)
        End Try
        window.Invalidate(True)
    End Sub

    Private Function Translate(value As String, byText As Dictionary(Of String, String), target As Dictionary(Of String, String)) As String
        If String.IsNullOrEmpty(value) Then Return value
        Dim key As String = Nothing
        If byText.TryGetValue(value, key) Then Return Localization.Multiline(target(key))
        ' Values embedded in a known localized template are data and survive the rebind.
        For Each pair In byText.Where(Function(p) p.Key.Contains("{0}"))
            Dim indices As New List(Of Integer)
            Dim pattern = Regex.Escape(pair.Key)
            pattern = Regex.Replace(pattern, "\\\{(\d+)\\\}", Function(m)
                                                                          indices.Add(Integer.Parse(m.Groups(1).Value))
                                                                          Return "(.*?)"
                                                                      End Function)
            If indices.Count = 0 Then Continue For
            Dim match = Regex.Match(value, "\A" & pattern & "\z", RegexOptions.Singleline)
            If Not match.Success Then Continue For
            Dim values(indices.Max()) As Object
            For i = 0 To indices.Count - 1
                values(indices(i)) = match.Groups(i + 1).Value
            Next
            Return Localization.Format(Localization.Multiline(target(pair.Value)), values)
        Next
        Return value
    End Function

    Private Sub Rebind(control As Control, byText As Dictionary(Of String, String), target As Dictionary(Of String, String))
        Dim rail = TryCast(control, RailEntry)
        If rail IsNot Nothing AndAlso target.ContainsKey(rail.Key) Then
            rail.Text = If(rail.IsGroupHeader, target(rail.Key).ToUpperInvariant(), target(rail.Key))
        ElseIf Not TypeOf control Is TextBoxBase AndAlso Not TypeOf control Is ComboBox Then
            control.Text = Translate(control.Text, byText, target)
        End If
        control.AccessibleName = Translate(control.AccessibleName, byText, target)
        control.AccessibleDescription = Translate(control.AccessibleDescription, byText, target)
        Dim combo = TryCast(control, ComboBox)
        If combo IsNot Nothing AndAlso combo.DropDownStyle = ComboBoxStyle.DropDownList Then
            For i = 0 To combo.Items.Count - 1
                If TypeOf combo.Items(i) Is String Then
                    Dim text = CStr(combo.Items(i))
                    Dim key As String = Nothing
                    If byText.TryGetValue(text, key) Then combo.Items(i) = Localization.Multiline(target(key))
                End If
            Next
        End If
        Dim list = TryCast(control, ListView)
        If list IsNot Nothing Then
            For Each column As ColumnHeader In list.Columns
                column.Text = Translate(column.Text, byText, target)
            Next
        End If
        If control.ContextMenuStrip IsNot Nothing Then RebindMenu(control.ContextMenuStrip.Items, byText, target)
        Dim type = control.GetType()
        While type IsNot Nothing AndAlso type.Assembly Is GetType(ShellForm).Assembly
            For Each field In type.GetFields(fields)
                Dim value = field.GetValue(control)
                Dim dictionary = TryCast(value, Dictionary(Of String, String))
                If field.Name = "dict" AndAlso dictionary IsNot Nothing Then
                    dictionary.Clear()
                    For Each pair In target
                        dictionary(pair.Key) = pair.Value
                    Next
                End If
                Dim tips = TryCast(value, ToolTip)
                If tips IsNot Nothing Then
                    For Each child In Ui.AllControls(control)
                        tips.SetToolTip(child, Translate(tips.GetToolTip(child), byText, target))
                    Next
                End If
                Dim menu = TryCast(value, ContextMenuStrip)
                If menu IsNot Nothing Then RebindMenu(menu.Items, byText, target)
            Next
            type = type.BaseType
        End While
        For Each child As Control In control.Controls
            Rebind(child, byText, target)
        Next
        Dim group = TryCast(control, SettingsGroup)
        If group IsNot Nothing Then group.UpdateAccessibility()
    End Sub

    Private Sub RebindMenu(items As ToolStripItemCollection, byText As Dictionary(Of String, String), target As Dictionary(Of String, String))
        For Each item As ToolStripItem In items
            item.Text = Translate(item.Text, byText, target)
            item.AccessibleName = Translate(item.AccessibleName, byText, target)
            item.ToolTipText = Translate(item.ToolTipText, byText, target)
            Dim menu = TryCast(item, ToolStripDropDownItem)
            If menu IsNot Nothing Then RebindMenu(menu.DropDownItems, byText, target)
        Next
    End Sub
End Module
