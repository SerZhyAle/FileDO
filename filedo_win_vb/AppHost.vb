Imports System.Runtime.InteropServices
Imports System.Threading

' The windows of one filedo_win.exe process (SP-0063 D1, amending SP-0006 5.1 D15): one shell
' window, plus the Disk Manager. Both live in this one process - single instance is kept - and
' share its runner, its journal, its theme and its job object; the process ends when the last of
' them has closed.
'
' The Disk Manager is a window of its own rather than a page of the shell because its whole point
' is state at a glance: a page would hide the moment another job was chosen.
'
' Three ways in (spec 4.2): the Disks rail row "Disk manager", the list job's "Open in Disk manager",
' and `filedo_win.exe --disks`, which starts straight into the manager with no shell window shown.
' A second `--disks` while this process runs asks it, by a registered window message, to bring the
' manager forward instead of opening a second copy.
Friend Class AppHost
    Inherits ApplicationContext

    ' The host of this process, or Nothing when none runs (the self-test builds windows on their own).
    Friend Shared Current As AppHost

    Private shell As ShellForm
    Private manager As DiskManagerForm
    Private listener As HostListener

    Friend Sub New(startShell As ShellForm, openManager As Boolean)
        Current = Me
        Try
            listener = New HostListener(Me)
        Catch ex As Exception
            ' Without it a second --disks opens a copy of its own; nothing else depends on it.
            ShellLog.Write("listen for a second start", ex)
        End Try
        If startShell IsNot Nothing Then
            AttachShell(startShell)
            startShell.Show()
        End If
        If openManager Then ShowManager()
    End Sub

    Private Sub AttachShell(s As ShellForm)
        shell = s
        AddHandler s.FormClosed, Sub()
                                     If shell Is s Then shell = Nothing
                                     ExitIfIdle()
                                 End Sub
    End Sub

    ' The shell, made when a delegated operation needs one and none is open.
    Private Function EnsureShell() As ShellForm
        If shell Is Nothing OrElse shell.IsDisposed Then AttachShell(New ShellForm())
        Return shell
    End Function

    ' The shell window, in front: the manager's "Main window" button and a plain second start come here.
    Friend Sub ShowShell()
        EnsureShell().BringBack()
    End Sub

    Friend Sub ShowManager()
        If manager Is Nothing OrElse manager.IsDisposed Then
            Dim m As New DiskManagerForm()
            manager = m
            AddHandler m.JobRequested, AddressOf OnManagerJob
            AddHandler m.ShellRequested, Sub() ShowShell()
            AddHandler m.FormClosed, Sub()
                                         If manager Is m Then manager = Nothing
                                         ExitIfIdle()
                                     End Sub
            m.Show()
        Else
            manager.BringBack()
        End If
    End Sub

    ' Spec 7.3: a delegated operation opens its job page in the shell, with the container chosen,
    ' and brings the shell forward.
    Private Sub OnManagerJob(key As String, target As String, preset As String)
        EnsureShell().OpenDiskJob(key, target, preset)
    End Sub

    Private Sub ExitIfIdle()
        Dim shellGone = shell Is Nothing OrElse shell.IsDisposed
        Dim managerGone = manager Is Nothing OrElse manager.IsDisposed
        If shellGone AndAlso managerGone Then
            If listener IsNot Nothing Then listener.DestroyHandle()
            listener = Nothing
            ExitThread()
        End If
    End Sub

    ' ---- what the shell tells the host --------------------------------------

    Friend Shared Sub OpenDiskManager()
        If Current IsNot Nothing Then Current.ShowManager()
    End Sub

    ' A run on the shell ended: what it changed is read (spec 7.3, "when that run ends, the manager
    ' refreshes").
    Friend Shared Sub ShellRunFinished()
        If Current Is Nothing Then Return
        Dim m = Current.manager
        If m IsNot Nothing AndAlso Not m.IsDisposed Then m.RequestRead()
    End Sub

    Friend Shared Sub ThemeChanged()
        If Current Is Nothing Then Return
        Dim m = Current.manager
        If m IsNot Nothing AndAlso Not m.IsDisposed Then m.ApplyTheme()
    End Sub

    ' ---- a second start (spec 4.2) ------------------------------------------

    <DllImport("user32.dll", CharSet:=CharSet.Unicode)>
    Private Shared Function RegisterWindowMessage(name As String) As Integer
    End Function

    Private Delegate Function EnumWindowsProc(hwnd As IntPtr, lParam As IntPtr) As Boolean

    <DllImport("user32.dll")>
    Private Shared Function EnumWindows(callback As EnumWindowsProc, lParam As IntPtr) As Boolean
    End Function

    <DllImport("user32.dll")>
    Private Shared Function GetWindowThreadProcessId(hwnd As IntPtr, ByRef pid As Integer) As Integer
    End Function

    <DllImport("user32.dll")>
    Private Shared Function AllowSetForegroundWindow(pid As Integer) As Boolean
    End Function

    <DllImport("user32.dll")>
    Private Shared Function SendMessageTimeout(hwnd As IntPtr, msg As Integer, wParam As IntPtr, lParam As IntPtr,
                                               flags As Integer, timeoutMs As Integer, ByRef result As IntPtr) As IntPtr
    End Function

    Private Const SMTO_ABORTIFHUNG As Integer = &H2

    Friend Shared ReadOnly ShowDisksMessage As Integer = RegisterWindowMessage("FileDO.Shell.ShowDiskManager")

    ' The other direction: a plain start - the Start menu's FileDO entry - while the running copy has
    ' only its Disk Manager open asks it for the shell window, which the old hand-over (focus whatever
    ' window is there) could not give: it would have raised the manager and left the user without the
    ' main window they clicked.
    Friend Shared ReadOnly ShowShellMessage As Integer = RegisterWindowMessage("FileDO.Shell.ShowShell")

    ' Asks the copy already running to bring its Disk Manager forward. True when one of its windows
    ' answered; False leaves the start to the ordinary hand-over.
    Friend Shared Function HandOverDisks() As Boolean
        Return HandOver(ShowDisksMessage)
    End Function

    ' Asks the copy already running to bring its shell window forward, opening it if it has none.
    Friend Shared Function HandOverShell() As Boolean
        Return HandOver(ShowShellMessage)
    End Function

    Private Shared Function HandOver(message As Integer) As Boolean
        If message = 0 Then Return False
        Try
            Using self = Process.GetCurrentProcess()
                Dim others = Process.GetProcessesByName(self.ProcessName)
                Try
                    For Each p In others
                        If p.Id = self.Id Then Continue For
                        ' The running copy may take the foreground: this start is the user's gesture.
                        AllowSetForegroundWindow(p.Id)
                        If AskWindowsOf(p.Id, message) Then Return True
                    Next
                Finally
                    For Each p In others
                        p.Dispose()
                    Next
                End Try
            End Using
        Catch ex As Exception
            ShellLog.Write("hand --disks to the running copy", ex)
        End Try
        Return False
    End Function

    Private Shared Function AskWindowsOf(pid As Integer, message As Integer) As Boolean
        Dim answered = False
        EnumWindows(Function(hwnd, l)
                        Dim owner = 0
                        GetWindowThreadProcessId(hwnd, owner)
                        If owner <> pid Then Return True
                        Dim result As IntPtr = IntPtr.Zero
                        If SendMessageTimeout(hwnd, message, IntPtr.Zero, IntPtr.Zero, SMTO_ABORTIFHUNG, 2000, result) <> IntPtr.Zero AndAlso
                           result = New IntPtr(1) Then
                            answered = True
                            Return False
                        End If
                        Return True
                    End Function, IntPtr.Zero)
        Return answered
    End Function

End Class

' A hidden top-level window that exists only to hear a second start's request. It answers at once
' and opens the manager after the answer, so the waiting start is never held by a window being built.
Friend Class HostListener
    Inherits NativeWindow

    Private ReadOnly host As AppHost

    Public Sub New(owner As AppHost)
        host = owner
        CreateHandle(New CreateParams())
    End Sub

    Protected Overrides Sub WndProc(ByRef m As Message)
        Dim wantsManager = (m.Msg = AppHost.ShowDisksMessage AndAlso AppHost.ShowDisksMessage <> 0)
        Dim wantsShell = (m.Msg = AppHost.ShowShellMessage AndAlso AppHost.ShowShellMessage <> 0)
        If wantsManager OrElse wantsShell Then
            m.Result = New IntPtr(1)
            Dim open As Action = If(wantsManager, CType(AddressOf host.ShowManager, Action), CType(AddressOf host.ShowShell, Action))
            Dim ctx = SynchronizationContext.Current
            If ctx IsNot Nothing Then
                ctx.Post(Sub(state) open(), Nothing)
            Else
                open()
            End If
            Return
        End If
        MyBase.WndProc(m)
    End Sub

End Class
