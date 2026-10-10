Imports System.Globalization
Imports System.Text
Imports System.Web.Script.Serialization

' Partition-backed virtual disks in the Disk Manager (SP-0148 section 10).
'
' A partition disk is a FileDO container that lives in a GPT partition FileDO created in what was
' unallocated space. The window learns about disks from filedo.exe alone, exactly as it learns the
' container rows: `filedo vd disks json` (schema filedo.vd-disks v1, cmd\filedo\vdisk_disks_windows.go)
' lists every disk, its partitions and its free extents with a usable flag and a reason token, and
' `vd status json` marks a partition row with carrier "partition" and its locator fdpart:{GUID}.
'
' What lives here: the reader of that document, the command lines of the partition verbs (built
' here and nowhere else), the disk map that draws one disk as a proportional bar, the new-partition
' dialog, the typed-name delete dialog and the adopt dialog. The manager (DiskManager.vb) runs every
' line through its own queue and Runner, as it runs every quick action.

' One run of unallocated space, as `vd disks json` lists it.
Public Class VdExtentInfo
    Public Property Offset As Long = 0
    Public Property Length As Long = 0
    Public Property Usable As Boolean = False
    ' A reason token (PartitionCommands.ReasonTokens), "" when usable.
    Public Property Reason As String = ""
End Class

' One partition entry of a disk's layout.
Public Class VdPartInfo
    Public Property Number As Integer = 0
    Public Property Guid As String = ""
    Public Property Offset As Long = 0
    Public Property Length As Long = 0
    ' A kind token: basic-data, efi, msr, recovery, ldm-metadata, ldm-data, spaces, spaces-replica,
    ' fileDO, mbr or other.
    Public Property Kind As String = ""
    Public Property FileDO As Boolean = False
    Public ReadOnly Property Letters As New List(Of String)
    ' The registered name of a FileDO partition, "" when this machine has none for it.
    Public Property Registered As String = ""
End Class

' One physical disk as discovery saw it.
Public Class VdDiskInfo
    Public Property Number As Integer = 0
    Public Property Guid As String = ""
    Public Property Model As String = ""
    Public Property Bus As String = ""
    Public Property Size As Long = 0
    Public Property Style As String = ""
    Public Property Online As Boolean = False
    Public Property Usable As Boolean = False
    Public Property Reason As String = ""
    Public Property Warning As String = ""
    Public ReadOnly Property Partitions As New List(Of VdPartInfo)
    Public ReadOnly Property Free As New List(Of VdExtentInfo)

    Public ReadOnly Property HasUsableFree As Boolean
        Get
            Return Usable AndAlso Free.Any(Function(f) f.Usable)
        End Get
    End Property
End Class

' The whole `vd disks json` document.
Public Class VdDisksDoc
    Public Const Schema As String = "filedo.vd-disks"
    Public Const KnownVersion As Integer = 1

    Public Property Available As Boolean = False
    ' "store-build" in the Microsoft Store build.
    Public Property Reason As String = ""
    Public ReadOnly Property Disks As New List(Of VdDiskInfo)

    ' Reads the document out of what filedo.exe printed: the one line that starts with {"schema" -
    ' the banner and the Finish: line around it are skipped. The compatibility law as for the
    ' snapshot: an unknown field is ignored, a missing one takes its default, and a schema or a
    ' version this build does not know is refused whole. Returns Nothing with problem set to a
    ' localization key when there is no document this build can read.
    Public Shared Function Parse(output As String, ByRef problem As String) As VdDisksDoc
        problem = ""
        Dim line As String = Nothing
        For Each raw In If(output, "").Split(New String() {vbCrLf, vbLf}, StringSplitOptions.RemoveEmptyEntries)
            Dim t = raw.Trim()
            If t.StartsWith("{""schema""", StringComparison.Ordinal) Then
                line = t
                Exit For
            End If
        Next
        If line Is Nothing Then
            problem = "vd_part_read_failed"
            Return Nothing
        End If
        Dim doc As Dictionary(Of String, Object)
        Try
            Dim js As New JavaScriptSerializer() With {.MaxJsonLength = Integer.MaxValue}
            doc = js.Deserialize(Of Dictionary(Of String, Object))(line)
        Catch ex As Exception
            ShellLog.Write("read the list of disks", ex)
            problem = "vd_part_read_failed"
            Return Nothing
        End Try
        If doc Is Nothing OrElse PartJson.Str(doc, "schema") <> Schema Then
            problem = "vd_part_read_failed"
            Return Nothing
        End If
        Dim version = CInt(PartJson.Num(doc, "version"))
        If version < 1 OrElse version > KnownVersion Then
            problem = "vd_part_read_failed"
            Return Nothing
        End If
        Dim out As New VdDisksDoc With {.Available = PartJson.Bool(doc, "available"), .Reason = PartJson.Str(doc, "reason")}
        For Each d In PartJson.Items(doc, "disks")
            Dim disk As New VdDiskInfo With {
                .Number = CInt(PartJson.Num(d, "number")),
                .Guid = PartitionCommands.NormGuid(PartJson.Str(d, "guid")),
                .Model = PartJson.Str(d, "model").Trim(),
                .Bus = PartJson.Str(d, "bus"),
                .Size = CLng(PartJson.Num(d, "size")),
                .Style = PartJson.Str(d, "style"),
                .Online = PartJson.Bool(d, "online"),
                .Usable = PartJson.Bool(d, "usable"),
                .Reason = PartJson.Str(d, "reason"),
                .Warning = PartJson.Str(d, "warning")}
            For Each p In PartJson.Items(d, "partitions")
                Dim part As New VdPartInfo With {
                    .Number = CInt(PartJson.Num(p, "number")),
                    .Guid = PartitionCommands.NormGuid(PartJson.Str(p, "guid")),
                    .Offset = CLng(PartJson.Num(p, "offset")),
                    .Length = CLng(PartJson.Num(p, "length")),
                    .Kind = PartJson.Str(p, "kind"),
                    .FileDO = PartJson.Bool(p, "fileDO"),
                    .Registered = PartJson.Str(p, "registered")}
                Dim letters = TryCast(PartJson.Value(p, "letters"), IEnumerable)
                If letters IsNot Nothing Then
                    For Each l In letters
                        Dim s = TryCast(l, String)
                        If Not String.IsNullOrEmpty(s) Then part.Letters.Add(s)
                    Next
                End If
                disk.Partitions.Add(part)
            Next
            For Each f In PartJson.Items(d, "free")
                disk.Free.Add(New VdExtentInfo With {
                    .Offset = CLng(PartJson.Num(f, "offset")),
                    .Length = CLng(PartJson.Num(f, "length")),
                    .Usable = PartJson.Bool(f, "usable"),
                    .Reason = PartJson.Str(f, "reason")})
            Next
            out.Disks.Add(disk)
        Next
        Return out
    End Function

    ' The disk and the entry of a partition GUID (braces and case do not matter), or False.
    Public Function FindPartition(guid As String, ByRef disk As VdDiskInfo, ByRef part As VdPartInfo) As Boolean
        Dim g = PartitionCommands.NormGuid(guid)
        disk = Nothing
        part = Nothing
        If g = "" Then Return False
        For Each d In Disks
            For Each p In d.Partitions
                If p.Guid = g Then
                    disk = d
                    part = p
                    Return True
                End If
            Next
        Next
        Return False
    End Function

    ' The FileDO partitions this machine has no name for (spec 4.6): what Adopt offers.
    Public Function Unregistered() As List(Of KeyValuePair(Of VdDiskInfo, VdPartInfo))
        Dim out As New List(Of KeyValuePair(Of VdDiskInfo, VdPartInfo))
        For Each d In Disks
            For Each p In d.Partitions
                If p.FileDO AndAlso p.Registered = "" AndAlso p.Guid <> "" Then out.Add(New KeyValuePair(Of VdDiskInfo, VdPartInfo)(d, p))
            Next
        Next
        Return out
    End Function
End Class

' The JSON values of the document, each with its default.
Friend NotInheritable Class PartJson
    Friend Shared Function Value(d As Dictionary(Of String, Object), key As String) As Object
        Dim v As Object = Nothing
        If d IsNot Nothing AndAlso d.TryGetValue(key, v) Then Return v
        Return Nothing
    End Function

    Friend Shared Function Str(d As Dictionary(Of String, Object), key As String) As String
        Return If(TryCast(Value(d, key), String), "")
    End Function

    Friend Shared Function Bool(d As Dictionary(Of String, Object), key As String) As Boolean
        Dim v = Value(d, key)
        Return TypeOf v Is Boolean AndAlso CBool(v)
    End Function

    Friend Shared Function Num(d As Dictionary(Of String, Object), key As String) As Decimal
        Dim v = Value(d, key)
        If v Is Nothing Then Return 0D
        If Not (TypeOf v Is Integer OrElse TypeOf v Is Long OrElse TypeOf v Is Decimal OrElse TypeOf v Is Double) Then Return 0D
        Try
            Return Convert.ToDecimal(v, CultureInfo.InvariantCulture)
        Catch
            Return 0D
        End Try
    End Function

    Friend Shared Iterator Function Items(d As Dictionary(Of String, Object), key As String) As IEnumerable(Of Dictionary(Of String, Object))
        Dim list = TryCast(Value(d, key), IEnumerable)
        If list Is Nothing Then Return
        For Each item In list
            Dim x = TryCast(item, Dictionary(Of String, Object))
            If x IsNot Nothing Then Yield x
        Next
    End Function
End Class

' The partition verbs' command lines (cli-surface.md, "Verbs") and the words around them. Every line
' a partition action runs is built here; a credential travels by variable name only, as on every
' Disks line.
Public NotInheritable Class PartitionCommands

    ' The smallest partition disk the console creates (64 MiB).
    Public Const MinBytes As Long = 64L << 20

    ' The line of the disk list, for the reader and the self-test.
    Friend Shared ReadOnly DisksArguments As String() = {"--no-history", "vd", "disks", "json"}

    ' The reason tokens of a disk or an extent that cannot hold a partition disk (spec 6.3).
    Public Shared ReadOnly ReasonTokens As String() = {"mbr", "not-initialized", "offline", "read-only", "iscsi", "spaces", "usb",
                                                "removable", "bus", "dynamic", "spaces-member", "duplicate-guid",
                                                "no-entry-slot", "too-small", "unreadable"}

    ' The kind words the map says for a partition, one per family of kind tokens.
    Public Shared ReadOnly KindKeys As String() = {"vd_part_kind_data", "vd_part_kind_efi", "vd_part_kind_msr", "vd_part_kind_recovery",
                                            "vd_part_kind_ldm", "vd_part_kind_spaces", "vd_part_kind_filedo", "vd_part_kind_other"}

    ' A GUID as it is compared: no braces, upper case.
    Public Shared Function NormGuid(s As String) As String
        Return If(s, "").Trim().Trim("{"c, "}"c).ToUpperInvariant()
    End Function

    ' The localized word of a reason token; a token this build does not know says "not supported".
    Public Shared Function ReasonKey(token As String) As String
        Dim t = If(token, "").Trim().ToLowerInvariant()
        If ReasonTokens.Contains(t) Then Return "vd_part_reason_" & t.Replace("-", "_")
        Return "vd_part_reason_other"
    End Function

    Public Shared Function KindKey(kind As String, isFileDO As Boolean) As String
        If isFileDO Then Return "vd_part_kind_filedo"
        Select Case If(kind, "").ToLowerInvariant()
            Case "basic-data" : Return "vd_part_kind_data"
            Case "efi" : Return "vd_part_kind_efi"
            Case "msr" : Return "vd_part_kind_msr"
            Case "recovery" : Return "vd_part_kind_recovery"
            Case "ldm-metadata", "ldm-data" : Return "vd_part_kind_ldm"
            Case "spaces", "spaces-replica" : Return "vd_part_kind_spaces"
            Case "filedo" : Return "vd_part_kind_filedo"
        End Select
        Return "vd_part_kind_other"
    End Function

    ' A position on a disk: GiB with two decimals - an extent's start is rarely a whole GiB.
    Public Shared Function OffsetText(n As Long) As String
        Return (n / CDbl(1L << 30)).ToString("0.00", CultureInfo.InvariantCulture) & " GiB"
    End Function

    ' `vd new part disk:{GUID} size <N>M|max at <offset> <profile> as <name> [label <text>] p:|pe:.. force`.
    ' sizeMiB 0 is "max", the whole extent. The prompt was answered by the window's own confirmation;
    ' force skips only that prompt, never a check (cli-surface.md).
    Public Shared Function NewPart(diskGuid As String, sizeMiB As Long, offset As Long, profile As String, name As String,
                            label As String, hasCredential As Boolean) As List(Of String)
        Dim a As New List(Of String) From {"vd", "new", "part", "disk:{" & NormGuid(diskGuid) & "}", "size",
                                           If(sizeMiB <= 0, "max", sizeMiB.ToString(CultureInfo.InvariantCulture) & "M"),
                                           "at", offset.ToString(CultureInfo.InvariantCulture), profile, "as", name}
        If Not String.IsNullOrWhiteSpace(label) Then
            a.Add("label")
            a.Add(label.Trim())
        End If
        a.Add(If(hasCredential, "pe:" & DiskCommands.CredentialEnvName, "p:"))
        a.Add("force")
        Return a
    End Function

    ' `vd image <name> to <new.fdd>` - never over an existing file, so no force.
    Public Shared Function Image(target As String, dest As String) As List(Of String)
        Return New List(Of String) From {"vd", "image", target, "to", dest}
    End Function

    ' `vd destroy <name> [wipe] force` - the name was typed in the window's own dialog.
    Public Shared Function Destroy(name As String, wipe As Boolean) As List(Of String)
        Dim a As New List(Of String) From {"vd", "destroy", name}
        If wipe Then a.Add("wipe")
        a.Add("force")
        Return a
    End Function

    ' `vd adopt fdpart:{GUID} as <name>`.
    Public Shared Function Adopt(partGuid As String, name As String) As List(Of String)
        Return New List(Of String) From {"vd", "adopt", "fdpart:{" & NormGuid(partGuid) & "}", "as", name}
    End Function

    ' The neighbour of an extent in words: the partition that ends where it starts (before) or starts
    ' where it ends (after) - its letter, or its kind - or the disk's edge.
    Public Shared Function Neighbour(d As VdDiskInfo, f As VdExtentInfo, before As Boolean, dict As Dictionary(Of String, String)) As String
        Dim best As VdPartInfo = Nothing
        For Each p In d.Partitions
            If before Then
                If p.Offset + p.Length <= f.Offset AndAlso (best Is Nothing OrElse p.Offset > best.Offset) Then best = p
            Else
                If p.Offset >= f.Offset + f.Length AndAlso (best Is Nothing OrElse p.Offset < best.Offset) Then best = p
            End If
        Next
        If best Is Nothing Then Return T(dict, If(before, "vd_part_edge_start", "vd_part_edge_end"))
        Return PartWord(best, dict)
    End Function

    ' A partition in a few words: its letters, else its kind.
    Public Shared Function PartWord(p As VdPartInfo, dict As Dictionary(Of String, String)) As String
        If p.Letters.Count > 0 Then Return String.Join(" ", p.Letters.ToArray())
        If p.FileDO AndAlso p.Registered <> "" Then Return p.Registered
        Return T(dict, KindKey(p.Kind, p.FileDO))
    End Function

    ' The words of a partition row where a file row has its path (spec 10: the disk's model and the
    ' extent), and the sentence of the detail pane.
    Public Shared Function PlaceText(d As VdDiskInfo, p As VdPartInfo, dict As Dictionary(Of String, String)) As String
        Return Localization.Format(T(dict, "vd_part_place_fmt"), ModelOf(d, dict), DiskStates.SizeText(p.Length), OffsetText(p.Offset))
    End Function

    Public Shared Function DetailText(d As VdDiskInfo, p As VdPartInfo, dict As Dictionary(Of String, String)) As String
        Return Localization.Format(T(dict, "vd_part_detail_fmt"), d.Number, ModelOf(d, dict), DiskStates.SizeText(p.Length), OffsetText(p.Offset))
    End Function

    Public Shared Function ModelOf(d As VdDiskInfo, dict As Dictionary(Of String, String)) As String
        Return If(d.Model <> "", d.Model, Localization.Format(T(dict, "vd_part_disk_unnamed_fmt"), d.Number))
    End Function

    ' Fills each partition row's place from the disk list, joined by the locator's GUID. A row whose
    ' partition the list does not hold says so rather than staying blank.
    Public Shared Sub Join(doc As VdDisksDoc, rows As IEnumerable(Of DiskRecord), dict As Dictionary(Of String, String))
        For Each r In rows
            If Not r.IsPartition Then Continue For
            Dim d As VdDiskInfo = Nothing
            Dim p As VdPartInfo = Nothing
            Dim guid = r.Locator
            If guid.StartsWith("fdpart:", StringComparison.OrdinalIgnoreCase) Then guid = guid.Substring(7)
            If doc IsNot Nothing AndAlso doc.FindPartition(guid, d, p) Then
                r.PartitionPlace = PlaceText(d, p, dict)
                r.PartitionDetail = DetailText(d, p, dict)
            Else
                r.PartitionPlace = T(dict, If(doc Is Nothing, "vd_part_place_unread", "vd_part_place_unlisted"))
                r.PartitionDetail = ""
            End If
        Next
    End Sub

    ' Asks filedo.exe for the disk list, off the window's thread, as DiskStateProbe asks for the
    ' snapshot: no window, stdin closed, in the window's job, killed when it has not answered.
    Friend Const TimeoutMs As Integer = 20000

    Public Shared Function Read(ByRef problem As String) As VdDisksDoc
        problem = ""
        If Not DiskProbe.Enabled Then
            problem = "vd_part_read_failed"
            Return Nothing
        End If
        Try
            Dim psi = Runner.NewStartInfo(Runner.LocateCLI(), ArgQuoting.JoinArgs(DisksArguments), Runner.GetAppDataDir(), False)
            Dim sb As New StringBuilder()
            Dim gate As New Object()
            Using p As New Process With {.StartInfo = psi}
                AddHandler p.OutputDataReceived, Sub(s, e)
                                                     If e.Data Is Nothing Then Return
                                                     SyncLock gate
                                                         sb.AppendLine(e.Data)
                                                     End SyncLock
                                                 End Sub
                AddHandler p.ErrorDataReceived, Sub(s, e)
                                                End Sub
                p.Start()
                ChildJob.Assign(p)
                p.BeginOutputReadLine()
                p.BeginErrorReadLine()
                Try
                    p.StandardInput.Close()
                Catch
                End Try
                If Not p.WaitForExit(TimeoutMs) Then
                    Try
                        p.Kill()
                    Catch
                    End Try
                    problem = "vd_part_read_failed"
                    Return Nothing
                End If
                p.WaitForExit()
                Dim text As String
                SyncLock gate
                    text = sb.ToString()
                End SyncLock
                If p.ExitCode <> 0 Then
                    ShellLog.Info("vd disks json ended with " & p.ExitCode.ToString())
                    problem = "vd_part_read_failed"
                    Return Nothing
                End If
                Return VdDisksDoc.Parse(text, problem)
            End Using
        Catch ex As Exception
            ShellLog.Write("list the disks", ex)
            problem = "vd_part_read_failed"
            Return Nothing
        End Try
    End Function

    Friend Shared Function T(dict As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return Localization.Multiline(v)
        Return key
    End Function

    ' Every key the partition surfaces use, for the self-test's five-language row.
    Friend Shared Function AllKeys() As List(Of String)
        Dim keys As New List(Of String) From {
            "vd_mgr_col_carrier", "vd_part_carrier_file", "vd_part_carrier_partition", "vd_part_new_choice_title",
            "vd_part_new_choice_text", "vd_part_new_choice_store_text", "vd_part_btn_file", "vd_part_btn_partition", "vd_part_store",
            "vd_part_read_failed", "vd_part_title", "vd_part_intro", "vd_part_no_disks", "vd_part_no_usable", "vd_part_disk_fmt",
            "vd_part_disk_unnamed_fmt", "vd_part_disk_refused_fmt", "vd_part_disk_warning_vhd", "vd_part_map_name_fmt",
            "vd_part_seg_free_fmt", "vd_part_seg_unusable_fmt", "vd_part_seg_part_fmt", "vd_part_edge_start", "vd_part_edge_end",
            "vd_part_selected_fmt", "vd_part_none_selected", "vd_part_lbl_size", "vd_part_unit_gib", "vd_part_unit_mib",
            "vd_part_size_max", "vd_part_lbl_profile", "vd_part_profile_fast", "vd_part_profile_plain", "vd_part_profile_vault",
            "vd_part_residue", "vd_part_fast_note", "vd_part_lbl_label", "vd_part_lbl_name", "vd_part_need_extent", "vd_part_need_size",
            "vd_part_too_small", "vd_part_too_big_fmt", "vd_part_bad_label", "vd_part_btn_create", "vd_part_facts", "vd_part_confirm_title",
            "vd_part_confirm_fmt", "vd_part_btn_create_go", "vd_part_place_fmt", "vd_part_place_unread", "vd_part_place_unlisted",
            "vd_part_detail_fmt", "vd_part_detail_locator_fmt", "vd_part_detail_consent", "vd_part_detail_refusals", "vd_part_state_missing",
            "vd_part_state_different", "vd_part_why_missing", "vd_part_why_different", "vd_part_detail_missing", "vd_part_detail_different",
            "vd_part_why_fixed_size", "vd_part_why_job_page", "vd_part_why_no_file", "vd_part_why_adopt", "vd_part_why_not_partition",
            "vd_mgr_act_image", "vd_mgr_act_adopt", "vd_part_image_exists", "vd_part_destroy_title",
            "vd_part_destroy_fmt", "vd_part_destroy_lbl", "vd_part_btn_delete", "vd_part_destroy_wipe", "vd_part_adopt_title",
            "vd_part_adopt_text", "vd_part_adopt_none", "vd_part_adopt_lbl", "vd_part_adopt_item_fmt", "vd_part_btn_adopt",
            "vd_part_reason_other"}
        For Each token In ReasonTokens
            keys.Add(ReasonKey(token))
        Next
        keys.AddRange(KindKeys)
        Return keys
    End Function

End Class

' One disk drawn as Disk Management draws it: a bar whose segments are its partitions and its free
' extents in their order, each as wide as its share of the disk (with a floor, so a 16 MiB reserved
' partition stays visible). A free extent that can hold a partition disk is selectable - by a click,
' or by the arrow keys when the bar has the focus; one that cannot is drawn greyed and says why in
' its tooltip. The layout is a function of the client size alone (Layout), so it holds at any DPI.
Friend Class DiskMapBar
    Inherits Control

    Friend Class Segment
        Public Bounds As Rectangle
        Public IsFree As Boolean
        ' The entry's index in the disk's Free or Partitions list.
        Public Index As Integer
        Public Usable As Boolean
    End Class

    Public Event SelectionChanged()

    ' The narrowest a segment is drawn, in design pixels.
    Friend Const MinSegmentDesign As Integer = 14

    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly tip As New ToolTip()
    Private segments As New List(Of Segment)
    Private selectedFree As Integer = -1
    Private hoverAt As Integer = -1
    Private shownTip As String = ""

    Public ReadOnly Property Disk As VdDiskInfo

    Public Sub New(d As VdDiskInfo, dictionary As Dictionary(Of String, String))
        Disk = d
        dict = dictionary
        SetStyle(ControlStyles.OptimizedDoubleBuffer Or ControlStyles.AllPaintingInWmPaint Or ControlStyles.UserPaint Or
                 ControlStyles.ResizeRedraw Or ControlStyles.Selectable, True)
        TabStop = d.HasUsableFree
        AccessibleRole = AccessibleRole.List
        AccessibleName = Localization.Format(PartitionCommands.T(dict, "vd_part_map_name_fmt"), d.Number)
    End Sub

    ' The free extent chosen, or Nothing.
    Public ReadOnly Property SelectedExtent As VdExtentInfo
        Get
            If selectedFree < 0 OrElse selectedFree >= Disk.Free.Count Then Return Nothing
            Return Disk.Free(selectedFree)
        End Get
    End Property

    Public Sub ClearSelection()
        If selectedFree < 0 Then Return
        selectedFree = -1
        Invalidate()
    End Sub

    ' Chooses a free extent by its index; an unusable one is never chosen.
    Public Function SelectFree(index As Integer) As Boolean
        If index < 0 OrElse index >= Disk.Free.Count OrElse Not Disk.Usable OrElse Not Disk.Free(index).Usable Then Return False
        If selectedFree <> index Then
            selectedFree = index
            Invalidate()
            RaiseEvent SelectionChanged()
        End If
        Return True
    End Function

    ' The segments for a client rectangle: in disk order, touching, never overlapping, the last one
    ' ending at the right edge, each at least minSeg wide while the width allows it.
    Friend Shared Function LayoutSegments(d As VdDiskInfo, area As Rectangle, minSeg As Integer) As List(Of Segment)
        Dim items As New List(Of KeyValuePair(Of Long, Segment))
        Dim lengths As New Dictionary(Of Segment, Long)
        For i = 0 To d.Partitions.Count - 1
            Dim s As New Segment With {.IsFree = False, .Index = i}
            items.Add(New KeyValuePair(Of Long, Segment)(d.Partitions(i).Offset, s))
            lengths(s) = Math.Max(1L, d.Partitions(i).Length)
        Next
        For i = 0 To d.Free.Count - 1
            Dim s As New Segment With {.IsFree = True, .Index = i, .Usable = d.Usable AndAlso d.Free(i).Usable}
            items.Add(New KeyValuePair(Of Long, Segment)(d.Free(i).Offset, s))
            lengths(s) = Math.Max(1L, d.Free(i).Length)
        Next
        Dim out As New List(Of Segment)
        If items.Count = 0 OrElse area.Width <= 0 OrElse area.Height <= 0 Then Return out
        items.Sort(Function(a, b) a.Key.CompareTo(b.Key))
        Dim n = items.Count
        Dim floor = Math.Max(0, Math.Min(minSeg, area.Width \ n))
        Dim spare = area.Width - floor * n
        Dim total As Double = 0
        For Each kv In items
            total += lengths(kv.Value)
        Next
        Dim cum As Double = 0
        Dim left = area.Left
        For i = 0 To n - 1
            Dim s = items(i).Value
            cum += lengths(s)
            Dim right = area.Left + floor * (i + 1) + CInt(Math.Floor(spare * cum / total))
            If i = n - 1 Then right = area.Right
            right = Math.Max(left, Math.Min(right, area.Right))
            s.Bounds = New Rectangle(left, area.Top, right - left, area.Height)
            out.Add(s)
            left = right
        Next
        Return out
    End Function

    Friend ReadOnly Property SegmentsForTest As List(Of Segment)
        Get
            Relayout()
            Return segments
        End Get
    End Property

    Private Sub Relayout()
        segments = LayoutSegments(Disk, ClientRectangle, Ui.Px(Me, MinSegmentDesign))
    End Sub

    Protected Overrides Sub OnSizeChanged(e As EventArgs)
        MyBase.OnSizeChanged(e)
        Relayout()
    End Sub

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        If segments.Count = 0 Then Relayout()
        Dim p = Theme.Current
        Dim g = e.Graphics
        Using backBrush As New SolidBrush(p.Surface)
            g.FillRectangle(backBrush, ClientRectangle)
        End Using
        For i = 0 To segments.Count - 1
            Dim s = segments(i)
            Dim r = s.Bounds
            If r.Width <= 0 Then Continue For
            Dim fill As Color
            Dim ink As Color
            Dim edge As Color = p.Border
            If s.IsFree Then
                If Not s.Usable Then
                    fill = p.Background
                    ink = p.TextDisabled
                ElseIf s.Index = selectedFree Then
                    fill = p.Accent
                    ink = p.AccentText
                    edge = p.Accent
                Else
                    fill = If(i = hoverAt, p.ControlHover, p.Surface)
                    ink = p.Link
                    edge = p.Accent
                End If
            Else
                fill = If(Disk.Partitions(s.Index).FileDO, p.SurfaceBand, p.SurfaceAlt)
                ink = p.Text
            End If
            Using b As New SolidBrush(fill)
                g.FillRectangle(b, r)
            End Using
            Using pen As New Pen(edge)
                g.DrawRectangle(pen, r.X, r.Y, Math.Max(0, r.Width - 1), Math.Max(0, r.Height - 1))
            End Using
            Dim caption = ShortCaption(s)
            Dim inner = Rectangle.Inflate(r, -Ui.Px(Me, 3), -Ui.Px(Me, 2))
            If inner.Width > 0 AndAlso TextRenderer.MeasureText(g, caption, Font).Width <= inner.Width Then
                TextRenderer.DrawText(g, caption, Font, inner, ink,
                                      TextFormatFlags.HorizontalCenter Or TextFormatFlags.VerticalCenter Or TextFormatFlags.SingleLine Or TextFormatFlags.NoPrefix)
            End If
        Next
        If Focused AndAlso ShowFocusCues Then
            Dim at = segments.FirstOrDefault(Function(s) s.IsFree AndAlso s.Index = selectedFree)
            Dim box = If(at Is Nothing, ClientRectangle, at.Bounds)
            ControlPaint.DrawFocusRectangle(g, Rectangle.Inflate(box, -Ui.Px(Me, 2), -Ui.Px(Me, 2)))
        End If
    End Sub

    ' What a segment says inside the bar when it fits: the size of a free extent, the letter or kind
    ' of a partition.
    Private Function ShortCaption(s As Segment) As String
        If s.IsFree Then Return DiskStates.SizeText(Disk.Free(s.Index).Length)
        Return PartitionCommands.PartWord(Disk.Partitions(s.Index), dict)
    End Function

    ' The tooltip of a segment: what it is, its size, and - for free space that cannot be used - why.
    Friend Function TipOf(s As Segment) As String
        If s.IsFree Then
            Dim f = Disk.Free(s.Index)
            If Not s.Usable Then
                Dim why = If(Disk.Usable, f.Reason, If(Disk.Reason <> "", Disk.Reason, f.Reason))
                Return Localization.Format(PartitionCommands.T(dict, "vd_part_seg_unusable_fmt"), DiskStates.SizeText(f.Length),
                                           PartitionCommands.T(dict, PartitionCommands.ReasonKey(why)))
            End If
            Return Localization.Format(PartitionCommands.T(dict, "vd_part_seg_free_fmt"), DiskStates.SizeText(f.Length),
                                       PartitionCommands.Neighbour(Disk, f, True, dict), PartitionCommands.Neighbour(Disk, f, False, dict))
        End If
        Dim p = Disk.Partitions(s.Index)
        Return Localization.Format(PartitionCommands.T(dict, "vd_part_seg_part_fmt"), PartitionCommands.PartWord(p, dict),
                                   PartitionCommands.T(dict, PartitionCommands.KindKey(p.Kind, p.FileDO)), DiskStates.SizeText(p.Length))
    End Function

    Private Function HitIndex(pt As Point) As Integer
        For i = 0 To segments.Count - 1
            If segments(i).Bounds.Contains(pt) Then Return i
        Next
        Return -1
    End Function

    Protected Overrides Sub OnMouseMove(e As MouseEventArgs)
        MyBase.OnMouseMove(e)
        Dim i = HitIndex(e.Location)
        If i <> hoverAt Then
            hoverAt = i
            Invalidate()
        End If
        Dim text = If(i < 0, "", TipOf(segments(i)))
        If text <> shownTip Then
            shownTip = text
            tip.SetToolTip(Me, text)
        End If
    End Sub

    Protected Overrides Sub OnMouseLeave(e As EventArgs)
        MyBase.OnMouseLeave(e)
        hoverAt = -1
        Invalidate()
    End Sub

    Protected Overrides Sub OnMouseClick(e As MouseEventArgs)
        MyBase.OnMouseClick(e)
        Dim i = HitIndex(e.Location)
        If i < 0 Then Return
        Dim s = segments(i)
        If s.IsFree AndAlso s.Usable Then
            Focus()
            SelectFree(s.Index)
        End If
    End Sub

    Protected Overrides Function IsInputKey(keyData As Keys) As Boolean
        If keyData = Keys.Left OrElse keyData = Keys.Right Then Return True
        Return MyBase.IsInputKey(keyData)
    End Function

    ' The arrow keys step through the usable extents; Space or Enter on the bar takes the first.
    Protected Overrides Sub OnKeyDown(e As KeyEventArgs)
        MyBase.OnKeyDown(e)
        Dim usable = Enumerable.Range(0, Disk.Free.Count).Where(Function(i) Disk.Usable AndAlso Disk.Free(i).Usable).ToList()
        If usable.Count = 0 Then Return
        Dim at = usable.IndexOf(selectedFree)
        Select Case e.KeyCode
            Case Keys.Right
                SelectFree(usable(If(at < 0, 0, Math.Min(usable.Count - 1, at + 1))))
                e.Handled = True
            Case Keys.Left
                SelectFree(usable(If(at < 0, 0, Math.Max(0, at - 1))))
                e.Handled = True
            Case Keys.Space
                If at < 0 Then SelectFree(usable(0))
                e.Handled = True
        End Select
    End Sub

    Protected Overrides Sub OnGotFocus(e As EventArgs)
        MyBase.OnGotFocus(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnLostFocus(e As EventArgs)
        MyBase.OnLostFocus(e)
        Invalidate()
    End Sub

    Protected Overrides Sub Dispose(disposing As Boolean)
        If disposing Then tip.Dispose()
        MyBase.Dispose(disposing)
    End Sub

End Class

' The partition branch of a new disk (SP-0148 section 10): each disk as a map, a usable free extent
' chosen on it, the size (a number in GiB or MiB, or all of it), the profile - fast by default; plain
' and vault say what the free space held before stays in their unused clusters - the label, the name
' in the list, and the password. Create asks once more, with the facts of spec 4.7 and the consent
' notice, before the dialog closes; the manager then runs `vd new part` through its queue.
Public Class DiskNewPartitionDialog
    Inherits DiskSmallDialog

    Private Const ShortPassword As Integer = 12

    Private ReadOnly maps As New List(Of DiskMapBar)
    Private ReadOnly taken As HashSet(Of String)
    Private ReadOnly chosenLabel As Label
    Private ReadOnly sizeBox As TextBox
    Private ReadOnly unitCombo As ComboBox
    Private ReadOnly maxCheck As CheckBox
    Private ReadOnly profileFast As RadioButton
    Private ReadOnly profilePlain As RadioButton
    Private ReadOnly profileVault As RadioButton
    Private ReadOnly profileNote As Label
    Private ReadOnly labelBox As TextBox
    Private ReadOnly nameBox As TextBox
    Private ReadOnly credBox As TextBox
    Private ReadOnly credConfirmBox As TextBox
    Private ReadOnly credShow As CheckBox
    Private ReadOnly credNote As Label
    Private ReadOnly verdict As Label
    Private nameTouched As Boolean = False
    Private settingName As Boolean = False
    Private captured As String = Nothing

    ' The confirmation the dialog asks before it closes. The self-test replaces it, so the dialog can
    ' be driven to its end without a window in front of it.
    Friend AskToCreate As Func(Of DialogSpec, Integer) = Function(spec) ShellDialog.Ask(Me, spec)

    Public Sub New(d As Dictionary(Of String, String), doc As VdDisksDoc, takenNames As HashSet(Of String), owner As Control)
        MyBase.New(d, Tr(d, "vd_part_title"), Tr(d, "vd_part_intro"), "vd_part_btn_create", owner)
        taken = If(takenNames, New HashSet(Of String)(StringComparer.OrdinalIgnoreCase))

        ' The disks, each a heading, what stands in its way, and its map.
        Dim disksHost As New TableLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .ColumnCount = 1,
                                                    .Margin = New Padding(0)}
        disksHost.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        Dim disks = If(doc Is Nothing, New List(Of VdDiskInfo), doc.Disks)
        For Each disk In disks
            Dim heading As New Label With {.AutoSize = True, .MaximumSize = New Size(P(460), 0), .Margin = PPad(0, 6, 0, 2),
                .Text = Localization.Format(T("vd_part_disk_fmt"), disk.Number, PartitionCommands.ModelOf(disk, dict),
                                            If(disk.Bus = "", "-", disk.Bus), DiskStates.SizeText(disk.Size), If(disk.Style = "", "-", disk.Style.ToUpperInvariant()))}
            AddRow(disksHost, heading)
            If Not disk.Usable Then
                AddRow(disksHost, NewNote(Localization.Format(T("vd_part_disk_refused_fmt"), T(PartitionCommands.ReasonKey(disk.Reason)))))
            End If
            If disk.Warning <> "" Then AddRow(disksHost, NewNote(T("vd_part_disk_warning_vhd")))
            Dim map As New DiskMapBar(disk, dict) With {.Size = New Size(P(460), P(34)), .Margin = PPad(0, 0, 0, 4)}
            Dim bar = map
            AddHandler map.SelectionChanged, Sub() OnMapChosen(bar)
            maps.Add(map)
            AddRow(disksHost, map)
        Next
        If disks.Count = 0 Then
            AddRow(disksHost, NewNote(T("vd_part_no_disks")))
        ElseIf Not disks.Any(Function(x) x.HasUsableFree) Then
            AddRow(disksHost, NewNote(T("vd_part_no_usable")))
        End If
        If disks.Count > 3 Then
            ' Many disks scroll inside the dialog rather than make it taller than the screen.
            Dim scroller As New Panel With {.AutoScroll = True, .Size = New Size(P(490), P(300)), .Margin = New Padding(0)}
            scroller.Controls.Add(disksHost)
            AddContent(scroller)
        Else
            AddContent(disksHost)
        End If

        chosenLabel = NewNote(T("vd_part_none_selected"))
        AddContent(chosenLabel)

        ' Size: a whole number in GiB or MiB, or the whole extent.
        Dim sizeRow = NewFlow()
        sizeRow.Controls.Add(NewLabel("vd_part_lbl_size"))
        sizeBox = New TextBox With {.Width = P(90), .Margin = PPad(0, 2, 6, 2)}
        sizeBox.AccessibleName = T("vd_part_lbl_size")
        unitCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = P(70), .Margin = PPad(0, 2, 12, 2)}
        unitCombo.Items.Add(T("vd_part_unit_gib"))
        unitCombo.Items.Add(T("vd_part_unit_mib"))
        unitCombo.SelectedIndex = 0
        unitCombo.AccessibleName = T("vd_part_lbl_size")
        maxCheck = New CheckBox With {.Text = T("vd_part_size_max"), .AutoSize = True, .Margin = PPad(0, 4, 0, 2)}
        sizeRow.Controls.Add(sizeBox)
        sizeRow.Controls.Add(unitCombo)
        sizeRow.Controls.Add(maxCheck)
        AddContent(sizeRow)

        ' Profile: fast by default (it overwrites the space at creation); plain and vault keep what the
        ' free space held before in their unused clusters, and say so.
        AddContent(NewLabel("vd_part_lbl_profile"))
        profileFast = NewRadio("vd_part_profile_fast")
        profilePlain = NewRadio("vd_part_profile_plain")
        profileVault = NewRadio("vd_part_profile_vault")
        profileFast.Checked = True
        AddContent(profileFast)
        AddContent(profilePlain)
        AddContent(profileVault)
        profileNote = NewNote("")
        AddContent(profileNote)

        Dim labelRow = NewFlow()
        labelRow.Controls.Add(NewLabel("vd_part_lbl_label"))
        labelBox = New TextBox With {.Width = P(200), .Margin = PPad(0, 2, 0, 2)}
        labelBox.AccessibleName = T("vd_part_lbl_label")
        labelRow.Controls.Add(labelBox)
        AddContent(labelRow)

        Dim nameRow = NewFlow()
        nameRow.Controls.Add(NewLabel("vd_part_lbl_name"))
        nameBox = New TextBox With {.Width = P(200), .Margin = PPad(0, 2, 0, 2)}
        nameBox.AccessibleName = T("vd_part_lbl_name")
        nameRow.Controls.Add(nameBox)
        AddContent(nameRow)

        ' The password, as the file branch asks it: twice, masked, with a show toggle and the honest
        ' line under it; empty is the visible obfuscated choice, except for a vault.
        AddContent(NewLabel("shell_lbl_password"))
        credBox = New TextBox With {.Width = P(300), .UseSystemPasswordChar = True, .Margin = PPad(0, 0, 0, 4)}
        credBox.AccessibleName = T("shell_lbl_password")
        AddContent(credBox)
        AddContent(NewLabel("shell_cred_confirm"))
        credConfirmBox = New TextBox With {.Width = P(300), .UseSystemPasswordChar = True, .Margin = PPad(0, 0, 0, 4)}
        credConfirmBox.AccessibleName = T("shell_cred_confirm")
        AddContent(credConfirmBox)
        credShow = New CheckBox With {.Text = T("shell_cred_show"), .AutoSize = True, .Margin = PPad(0, 0, 0, 4)}
        AddHandler credShow.CheckedChanged, Sub()
                                                credBox.UseSystemPasswordChar = Not credShow.Checked
                                                credConfirmBox.UseSystemPasswordChar = Not credShow.Checked
                                            End Sub
        AddContent(credShow)
        credNote = NewNote("")
        AddContent(credNote)
        AddContent(NewNote(T("shell_cred_out_of_sight")))

        ' The facts every surface states (spec 4.7), and why Create is not offered yet.
        AddContent(NewNote(T("vd_part_facts")))
        verdict = New Label With {.AutoSize = True, .MaximumSize = New Size(P(460), 0), .Margin = PPad(0, 6, 0, 0)}
        AddContent(verdict)

        For Each c As Control In New Control() {sizeBox, labelBox, credBox, credConfirmBox}
            AddHandler c.TextChanged, Sub() Revalidate()
        Next
        AddHandler nameBox.TextChanged, Sub()
                                            If Not settingName Then nameTouched = True
                                            Revalidate()
                                        End Sub
        AddHandler labelBox.TextChanged, Sub() SuggestName()
        AddHandler unitCombo.SelectedIndexChanged, Sub() Revalidate()
        AddHandler maxCheck.CheckedChanged, Sub()
                                                sizeBox.Enabled = Not maxCheck.Checked
                                                unitCombo.Enabled = Not maxCheck.Checked
                                                Revalidate()
                                            End Sub
        For Each rb In New RadioButton() {profileFast, profilePlain, profileVault}
            AddHandler rb.CheckedChanged, Sub() Revalidate()
        Next
        AddHandler okBtn.Click, AddressOf Create_Click

        ' The first usable extent is chosen to begin with, and all of it.
        For Each m In maps
            For i = 0 To m.Disk.Free.Count - 1
                If m.SelectFree(i) Then Exit For
            Next
            If m.SelectedExtent IsNot Nothing Then Exit For
        Next
        maxCheck.Checked = True
        SuggestName()
        Revalidate()
        Finish()
        ThemeExtras()
    End Sub

    Private Shared Sub AddRow(host As TableLayoutPanel, c As Control)
        host.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        host.Controls.Add(c, 0, host.RowStyles.Count - 1)
    End Sub

    Private Function NewFlow() As FlowLayoutPanel
        Return New FlowLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .WrapContents = True,
                                         .Margin = PPad(0, 4, 0, 2)}
    End Function

    Private Function NewLabel(key As String) As Label
        Return New Label With {.Text = T(key), .AutoSize = True, .Margin = PPad(0, 6, 6, 2)}
    End Function

    Private Function NewRadio(key As String) As RadioButton
        Return New RadioButton With {.Text = T(key), .AutoSize = True, .MaximumSize = New Size(P(460), 0), .Margin = PPad(0, 1, 0, 1)}
    End Function

    ' What DiskSmallDialog's theme does not reach: the radio buttons.
    Private Sub ThemeExtras()
        Dim p = Theme.Current
        For Each rb In New RadioButton() {profileFast, profilePlain, profileVault}
            rb.ForeColor = p.Text
            rb.BackColor = p.Surface
        Next
        verdict.ForeColor = p.Text
        verdict.Font = Theme.FontBodyStrong()
    End Sub

    ' Choosing an extent on one disk lets go of the choice on every other.
    Private Sub OnMapChosen(chosen As DiskMapBar)
        For Each m In maps
            If m IsNot chosen Then m.ClearSelection()
        Next
        Revalidate()
    End Sub

    Private Function ChosenMap() As DiskMapBar
        Return maps.FirstOrDefault(Function(m) m.SelectedExtent IsNot Nothing)
    End Function

    ' The default name: the label when it is a usable name, else "partition-disk" - made free.
    Private Sub SuggestName()
        If nameTouched Then Return
        Dim baseName = If(labelBox.Text.Trim() <> "", labelBox.Text.Trim(), "partition-disk")
        settingName = True
        nameBox.Text = DiskNameDialog.Suggest(baseName, taken)
        settingName = False
    End Sub

    Private Function Profile() As String
        If profilePlain.Checked Then Return "plain"
        If profileVault.Checked Then Return "vault"
        Return "fast"
    End Function

    ' The size in MiB the line will carry: 0 for "max", -1 when the field does not hold a whole number.
    Private Function SizeMiB() As Long
        If maxCheck.Checked Then Return 0
        Dim n As Long
        If Not Long.TryParse(sizeBox.Text.Trim(), NumberStyles.None, CultureInfo.InvariantCulture, n) OrElse n <= 0 Then Return -1
        If unitCombo.SelectedIndex = 0 Then
            If n > (Long.MaxValue >> 11) Then Return -1
            n *= 1024
        End If
        Return n
    End Function

    ' Why Create is not offered now, or "".
    Friend Function BlockReason() As String
        Dim m = ChosenMap()
        If m Is Nothing Then Return T("vd_part_need_extent")
        Dim f = m.SelectedExtent
        Dim mib = SizeMiB()
        If mib < 0 Then Return T("vd_part_need_size")
        Dim bytes = If(mib = 0, (f.Length >> 20) << 20, mib << 20)
        If bytes < PartitionCommands.MinBytes Then Return T("vd_part_too_small")
        If bytes > f.Length Then Return Localization.Format(T("vd_part_too_big_fmt"), DiskStates.SizeText((f.Length >> 20) << 20))
        Dim label = labelBox.Text.Trim()
        If label.Length > 32 OrElse label.IndexOfAny(New Char() {""""c, "|"c, "\"c, "/"c, ":"c, "*"c, "?"c, "<"c, ">"c}) >= 0 Then Return T("vd_part_bad_label")
        Dim n = nameBox.Text.Trim()
        If Not DiskStates.IsUsableName(n) Then Return T("vd_mgr_add_bad_name")
        If taken.Contains(n) Then Return Localization.Format(T("vd_mgr_add_taken_fmt"), n)
        If profileVault.Checked AndAlso credBox.Text = "" Then Return T("vd_cred_vault_needs")
        If credBox.Text <> credConfirmBox.Text Then Return T("shell_cred_mismatch")
        Return ""
    End Function

    Private Sub Revalidate()
        Dim m = ChosenMap()
        If m Is Nothing Then
            chosenLabel.Text = T("vd_part_none_selected")
        Else
            Dim f = m.SelectedExtent
            chosenLabel.Text = Localization.Format(T("vd_part_selected_fmt"), DiskStates.SizeText(f.Length), m.Disk.Number,
                                                   PartitionCommands.Neighbour(m.Disk, f, True, dict), PartitionCommands.Neighbour(m.Disk, f, False, dict))
        End If
        profileNote.Text = If(profileFast.Checked, T("vd_part_fast_note"), T("vd_part_residue"))
        If credBox.Text = "" Then
            credNote.Text = If(profileVault.Checked, T("vd_cred_vault_needs"), T("vd_cred_empty_new"))
        ElseIf credConfirmBox.Text <> credBox.Text Then
            credNote.Text = T("shell_cred_mismatch")
        ElseIf credBox.Text.Length < ShortPassword Then
            credNote.Text = T("shell_cred_short")
        Else
            credNote.Text = T("vd_cred_ok_new")
        End If
        Dim why = BlockReason()
        verdict.Text = why
        verdict.Visible = (why <> "")
        SetAcceptable(why = "")
    End Sub

    ' The question before anything is written (spec 4.2): the disk, the extent and its neighbours,
    ' the new partition, and the sentence that nothing outside the free space changes - with Cancel
    ' the default. Declined, the dialog stays open with everything typed.
    Private Sub Create_Click(sender As Object, e As EventArgs)
        If BlockReason() <> "" Then
            DialogResult = DialogResult.None
            Return
        End If
        If AskToCreate(ConfirmSpec()) <> 0 Then
            DialogResult = DialogResult.None
            Return
        End If
        captured = credBox.Text
        credBox.Text = ""
        credConfirmBox.Text = ""
    End Sub

    Friend Function ConfirmSpec() As DialogSpec
        Dim m = ChosenMap()
        Dim f = m.SelectedExtent
        Dim mib = SizeMiB()
        Dim bytes = If(mib = 0, (f.Length >> 20) << 20, mib << 20)
        Dim text = Localization.Format(T("vd_part_confirm_fmt"), m.Disk.Number, PartitionCommands.ModelOf(m.Disk, dict),
                                       If(m.Disk.Bus = "", "-", m.Disk.Bus), DiskStates.SizeText(m.Disk.Size), "{" & m.Disk.Guid & "}",
                                       PartitionCommands.OffsetText(f.Offset), PartitionCommands.OffsetText(f.Offset + f.Length),
                                       PartitionCommands.Neighbour(m.Disk, f, True, dict), PartitionCommands.Neighbour(m.Disk, f, False, dict),
                                       DiskStates.SizeText(bytes), Profile(), ChosenName)
        Return DestructiveDialogs.CreatePartition(dict, text)
    End Function

    ' ---- the answers --------------------------------------------------------------

    Public ReadOnly Property DiskGuid As String
        Get
            Dim m = ChosenMap()
            Return If(m Is Nothing, "", m.Disk.Guid)
        End Get
    End Property

    Public ReadOnly Property ChosenDisk As VdDiskInfo
        Get
            Dim m = ChosenMap()
            Return If(m Is Nothing, Nothing, m.Disk)
        End Get
    End Property

    Public ReadOnly Property ChosenExtent As VdExtentInfo
        Get
            Dim m = ChosenMap()
            Return If(m Is Nothing, Nothing, m.SelectedExtent)
        End Get
    End Property

    Public ReadOnly Property ChosenName As String
        Get
            Return nameBox.Text.Trim()
        End Get
    End Property

    Public ReadOnly Property HasCredential As Boolean
        Get
            Return captured IsNot Nothing AndAlso captured <> ""
        End Get
    End Property

    ' The console line, with the password by variable name only.
    Public Function CommandLine() As List(Of String)
        Dim f = ChosenExtent
        If f Is Nothing Then Return Nothing
        Dim hasCred = If(captured IsNot Nothing, captured <> "", credBox.Text <> "")
        Return PartitionCommands.NewPart(DiskGuid, Math.Max(0L, SizeMiB()), f.Offset, Profile(), ChosenName, labelBox.Text, hasCred)
    End Function

    ' The password, once: a second call returns "".
    Public Function TakePassword() As String
        Dim p = If(captured, "")
        captured = Nothing
        Return p
    End Function

    Protected Overrides Sub OnFormClosed(e As FormClosedEventArgs)
        credBox.Text = ""
        credConfirmBox.Text = ""
        MyBase.OnFormClosed(e)
    End Sub

    ' ---- for the self-test ------------------------------------------------------------

    Friend ReadOnly Property MapsForTest As List(Of DiskMapBar)
        Get
            Return maps
        End Get
    End Property

    Friend Sub SetForTest(sizeText As String, useMax As Boolean, profileName As String, name As String, password As String,
                          Optional inMiB As Boolean = False)
        maxCheck.Checked = useMax
        unitCombo.SelectedIndex = If(inMiB, 1, 0)
        sizeBox.Text = sizeText
        Select Case profileName
            Case "plain" : profilePlain.Checked = True
            Case "vault" : profileVault.Checked = True
            Case Else : profileFast.Checked = True
        End Select
        If name IsNot Nothing Then nameBox.Text = name
        credBox.Text = password
        credConfirmBox.Text = password
        Revalidate()
    End Sub

    Friend ReadOnly Property CreateEnabledForTest As Boolean
        Get
            Return okBtn.Enabled
        End Get
    End Property

    Friend ReadOnly Property ProfileNoteForTest As String
        Get
            Return profileNote.Text
        End Get
    End Property

End Class

' Delete a partition disk (spec 4.5): the partition in words, and the disk's name typed to confirm -
' the pattern of WIPE. Cancel is the default (Enter and Escape both give it); Delete is painted
' danger and is offered only while the typed name matches exactly.
Public Class DiskDestroyPartitionDialog
    Inherits DiskSmallDialog

    Private ReadOnly box As TextBox
    Private ReadOnly wipeCheck As CheckBox
    Private ReadOnly expected As String

    Public Sub New(d As Dictionary(Of String, String), name As String, place As String, owner As Control)
        MyBase.New(d, Tr(d, "vd_part_destroy_title"),
                   Localization.Format(Tr(d, "vd_part_destroy_fmt"), name, If(String.IsNullOrEmpty(place), name, place)),
                   "vd_part_btn_delete", owner)
        expected = If(name, "")
        Dim label As New Label With {.Text = T("vd_part_destroy_lbl"), .AutoSize = True, .Margin = PPad(0, 2, 0, 2)}
        box = New TextBox With {.Width = P(240), .Margin = PPad(0, 0, 0, 4)}
        box.AccessibleName = T("vd_part_destroy_lbl")
        wipeCheck = New CheckBox With {.Text = T("vd_part_destroy_wipe"), .AutoSize = True, .MaximumSize = New Size(P(460), 0),
                                       .Margin = PPad(0, 4, 0, 2)}
        AddHandler box.TextChanged, Sub() SetAcceptable(Matches())
        AddContent(label)
        AddContent(box)
        AddContent(wipeCheck)
        SetAcceptable(False)
        Finish()
        ' The safe answer is the default: Enter in the box is Cancel, never Delete.
        AcceptButton = cancelBtn
        okBtn.DialogResult = DialogResult.OK
        Dim pal = Theme.Current
        okBtn.Font = Theme.FontBody()
        Ui.StyleButton(okBtn, pal.Danger, pal.AccentText, pal.Danger)
        cancelBtn.Font = Theme.FontBodyStrong()
        ActiveControl = box
    End Sub

    Private Function Matches() As Boolean
        Return expected <> "" AndAlso String.Equals(box.Text.Trim(), expected, StringComparison.Ordinal)
    End Function

    Public ReadOnly Property Wipe As Boolean
        Get
            Return wipeCheck.Checked
        End Get
    End Property

    ' For the self-test: the button Enter presses, whether Delete is offered, and typing.
    Friend ReadOnly Property DefaultButtonForTest As String
        Get
            Dim b = TryCast(AcceptButton, Button)
            Return If(b Is Nothing, "", b.Text)
        End Get
    End Property

    Friend ReadOnly Property EscapeButtonForTest As String
        Get
            Dim b = TryCast(CancelButton, Button)
            Return If(b Is Nothing, "", b.Text)
        End Get
    End Property

    Friend ReadOnly Property DeleteEnabledForTest As Boolean
        Get
            Return okBtn.Enabled
        End Get
    End Property

    Friend Sub TypeForTest(text As String)
        box.Text = text
    End Sub

End Class

' Adopt (spec 4.6): the FileDO partitions on this computer's disks that the list has no name for,
' one chosen, and the name it is added under. The console reads its header (one consent prompt)
' and registers it.
Public Class DiskAdoptDialog
    Inherits DiskSmallDialog

    Private ReadOnly found As List(Of KeyValuePair(Of VdDiskInfo, VdPartInfo))
    Private ReadOnly combo As ComboBox
    Private ReadOnly box As TextBox
    Private ReadOnly verdict As Label
    Private ReadOnly taken As HashSet(Of String)

    Public Sub New(d As Dictionary(Of String, String), candidates As List(Of KeyValuePair(Of VdDiskInfo, VdPartInfo)),
                   takenNames As HashSet(Of String), owner As Control)
        MyBase.New(d, Tr(d, "vd_part_adopt_title"), Tr(d, "vd_part_adopt_text"), "vd_part_btn_adopt", owner)
        found = If(candidates, New List(Of KeyValuePair(Of VdDiskInfo, VdPartInfo)))
        taken = If(takenNames, New HashSet(Of String)(StringComparer.OrdinalIgnoreCase))
        Dim label As New Label With {.Text = T("vd_part_adopt_lbl"), .AutoSize = True, .Margin = PPad(0, 2, 0, 2)}
        combo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = P(420), .Margin = PPad(0, 0, 0, 6)}
        combo.AccessibleName = T("vd_part_adopt_lbl")
        For Each kv In found
            combo.Items.Add(Localization.Format(T("vd_part_adopt_item_fmt"), kv.Key.Number, PartitionCommands.ModelOf(kv.Key, dict),
                                                DiskStates.SizeText(kv.Value.Length), PartitionCommands.OffsetText(kv.Value.Offset)))
        Next
        If combo.Items.Count > 0 Then combo.SelectedIndex = 0
        Dim nameLabel As New Label With {.Text = T("vd_mgr_add_name"), .AutoSize = True, .Margin = PPad(0, 2, 0, 2)}
        box = New TextBox With {.Width = P(240), .Text = DiskNameDialog.Suggest("partition-disk", taken), .Margin = PPad(0, 0, 0, 4)}
        box.AccessibleName = T("vd_mgr_add_name")
        verdict = NewNote("")
        AddHandler box.TextChanged, Sub() Revalidate()
        AddHandler combo.SelectedIndexChanged, Sub() Revalidate()
        AddContent(label)
        AddContent(combo)
        AddContent(nameLabel)
        AddContent(box)
        AddContent(verdict)
        Revalidate()
        Finish()
        ActiveControl = box
    End Sub

    Private Sub Revalidate()
        Dim n = box.Text.Trim()
        If combo.SelectedIndex < 0 Then
            verdict.Text = T("vd_part_adopt_none")
            SetAcceptable(False)
        ElseIf Not DiskStates.IsUsableName(n) Then
            verdict.Text = T("vd_mgr_add_bad_name")
            SetAcceptable(False)
        ElseIf taken.Contains(n) Then
            verdict.Text = Localization.Format(T("vd_mgr_add_taken_fmt"), n)
            SetAcceptable(False)
        Else
            verdict.Text = T("vd_mgr_add_name_ok")
            SetAcceptable(True)
        End If
    End Sub

    Public ReadOnly Property ChosenGuid As String
        Get
            If combo.SelectedIndex < 0 Then Return ""
            Return found(combo.SelectedIndex).Value.Guid
        End Get
    End Property

    Public ReadOnly Property ChosenName As String
        Get
            Return box.Text.Trim()
        End Get
    End Property

End Class
