Imports System.IO
Imports System.Threading
Imports System.Threading.Tasks

' Step 2 of a drive job: the rows of the target box.
'
' Drives carry their label, file system, size and free space on the row, because a letter alone is not
' enough to tell two external disks apart - and picking the wrong one is the whole risk the Erase group
' is about (SP-0006 section 5.3). Those facts come from the drive itself, and a disconnected network
' drive answers after many seconds, so the page lists the letters at once and reads each drive on a
' thread of its own (SETTINGS-NAVIGATION 5.2, FILEDO-0253).
Partial Public Class JobView

    ' What one drive said. Ready = False leaves its row as the bare letter.
    Friend NotInheritable Class DriveFacts
        Public Ready As Boolean
        Public Label As String = ""
        Public FileSystem As String = ""
        Public SizeGb As Double
        Public FreeGb As Double
    End Class

    ' False in the self-test, which reads no drive and hands the page the facts itself.
    Friend Shared Property DriveReadsEnabled As Boolean = True

    ' At most one read per drive at a time: a page chosen again while a dead drive is still being asked
    ' waits on that read instead of holding another thread.
    Private Shared ReadOnly driveReads As New Dictionary(Of String, Task(Of DriveFacts))(StringComparer.OrdinalIgnoreCase)

    ' Facts (list generation, row, answer) not on screen yet; a page with no window yet keeps them here.
    Private ReadOnly arrivedFacts As New System.Collections.Concurrent.ConcurrentQueue(Of Tuple(Of Integer, Integer, DriveFacts))()

    Private driveListGeneration As Integer = 0
    Private rewritingDriveRow As Boolean = False

    ' The target box holds text that did not come from the list - typed, dropped, browsed, set by the
    ' window. Arriving facts never rewrite it.
    Private targetFromOutside As Boolean = False

    Private Sub PopulateTargets()
        driveListGeneration += 1
        targetCombo.Items.Clear()
        targetFromOutside = False
        If job Is Nothing Then Return
        If job.TargetKind <> JobDefinition.TargetType.Drive Then
            targetCombo.Text = ""
            Return
        End If

        Dim letters As New List(Of String)
        Try
            ' The letters come from Windows' own table; no drive is touched for them.
            For Each d In DriveInfo.GetDrives()
                letters.Add(d.Name)
            Next
        Catch ex As Exception
            ' The box still takes a typed path.
            ShellLog.Write("list drives", ex)
        End Try
        For Each letter In letters
            targetCombo.Items.Add(letter)
        Next

        If targetCombo.Items.Count > 0 Then targetCombo.SelectedIndex = 0
        If DriveReadsEnabled Then StartDriveReads(letters, driveListGeneration)
    End Sub

    Private Sub StartDriveReads(letters As List(Of String), generation As Integer)
        For i = 0 To letters.Count - 1
            Dim row = i
            ReadDriveOnce(letters(i)).ContinueWith(
                Sub(read As Task(Of DriveFacts))
                    arrivedFacts.Enqueue(Tuple.Create(generation, row, read.Result))
                    PostToUi(AddressOf ShowArrivedDriveFacts)
                End Sub, TaskScheduler.Default)
        Next
    End Sub

    Private Shared Function ReadDriveOnce(name As String) As Task(Of DriveFacts)
        SyncLock driveReads
            Dim running As Task(Of DriveFacts) = Nothing
            If driveReads.TryGetValue(name, running) AndAlso Not running.IsCompleted Then Return running
            ' A thread of its own: a read that hangs must not starve the pool the page's other reads use.
            running = Task.Factory.StartNew(Of DriveFacts)(Function() ReadDrive(name), CancellationToken.None,
                                                           TaskCreationOptions.LongRunning, TaskScheduler.Default)
            driveReads(name) = running
            Return running
        End SyncLock
    End Function

    Private Shared Function ReadDrive(name As String) As DriveFacts
        Dim facts As New DriveFacts()
        Try
            Dim d As New DriveInfo(name)
            If Not d.IsReady Then Return facts
            facts.Label = If(String.IsNullOrEmpty(d.VolumeLabel), d.DriveType.ToString(), d.VolumeLabel)
            facts.FileSystem = d.DriveFormat
            facts.SizeGb = d.TotalSize / (1024.0 * 1024.0 * 1024.0)
            facts.FreeGb = d.TotalFreeSpace / (1024.0 * 1024.0 * 1024.0)
            facts.Ready = True
        Catch ex As Exception
            ' A drive that vanishes while it is read keeps its bare letter.
            ShellLog.Write("read drive", ex)
        End Try
        Return facts
    End Function

    ' Facts that arrived before the page had a window are shown as soon as it has one.
    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        If Not arrivedFacts.IsEmpty Then BeginInvoke(New MethodInvoker(AddressOf ShowArrivedDriveFacts))
    End Sub

    Private Sub ShowArrivedDriveFacts()
        Dim arrived As Tuple(Of Integer, Integer, DriveFacts) = Nothing
        While arrivedFacts.TryDequeue(arrived)
            ApplyDriveFacts(arrived.Item1, arrived.Item2, arrived.Item3)
        End While
    End Sub

    ' One row gains its facts, on the window's thread. The box follows only while it shows that row as
    ' the list gave it; any other text - typed, set from outside, another row - stays as it was. An
    ' answer for an earlier page's list is dropped.
    Private Sub ApplyDriveFacts(generation As Integer, index As Integer, facts As DriveFacts)
        If generation <> driveListGeneration OrElse Not facts.Ready Then Return
        If index < 0 OrElse index >= targetCombo.Items.Count Then Return

        Dim bare = CStr(targetCombo.Items(index))
        Dim row = Localization.Format(L("shell_drive_row_fmt"), bare, facts.Label, facts.FileSystem, facts.SizeGb, facts.FreeGb)
        Dim shown = targetCombo.Text
        Dim follows = Not targetFromOutside AndAlso String.Equals(shown, bare, StringComparison.Ordinal)
        Dim typing = targetCombo.IsHandleCreated AndAlso targetCombo.Focused
        Dim caret = If(typing, targetCombo.SelectionStart, 0)
        Dim selected = If(typing, targetCombo.SelectionLength, 0)

        rewritingDriveRow = True
        Try
            ' Replacing the selected item rewrites the box, so what the box must hold is put back.
            targetCombo.Items(index) = row
            If follows Then
                targetCombo.SelectedIndex = index
                targetCombo.Text = row
            Else
                If targetCombo.Text <> shown Then targetCombo.Text = shown
                If typing Then targetCombo.Select(caret, selected)
            End If
        Finally
            rewritingDriveRow = False
        End Try

        ' The row a typed text matched may read differently now; the plan says what Run would take.
        If Not runner.IsActive Then UpdatePlanCard()
    End Sub

    ' "E:" when the box shows a row picked from this page's drive list, Nothing for any other text.
    Private Function ListedDriveInBox() As String
        If job Is Nothing OrElse job.TargetKind <> JobDefinition.TargetType.Drive OrElse targetFromOutside Then Return Nothing
        Dim text = targetCombo.Text
        For Each item In targetCombo.Items
            Dim row = TryCast(item, String)
            If row IsNot Nothing AndAlso row.Length >= 2 AndAlso row(1) = ":"c AndAlso
               String.Equals(row, text, StringComparison.Ordinal) Then Return row.Substring(0, 2)
        Next
        Return Nothing
    End Function

    ' Chooses a drive's row, as a pick from the list would. False when no row holds that drive.
    Private Function SelectDriveRow(drive As String) As Boolean
        For i = 0 To targetCombo.Items.Count - 1
            If CStr(targetCombo.Items(i)).StartsWith(drive, StringComparison.OrdinalIgnoreCase) Then
                targetFromOutside = False
                targetCombo.SelectedIndex = i
                Return True
            End If
        Next
        Return False
    End Function

    ' ---- the self-test's view ---------------------------------------------

    Friend ReadOnly Property DriveRowsForTest As String()
        Get
            Return targetCombo.Items.Cast(Of Object)().Select(Function(o) o.ToString()).ToArray()
        End Get
    End Property

    Friend ReadOnly Property DriveListGenerationForTest As Integer
        Get
            Return driveListGeneration
        End Get
    End Property

    Friend Sub ShowDriveFactsForTest(generation As Integer, index As Integer, facts As DriveFacts)
        ApplyDriveFacts(generation, index, facts)
    End Sub

    Friend Sub PickDriveRowForTest(index As Integer)
        targetFromOutside = False
        targetCombo.SelectedIndex = index
    End Sub

End Class
