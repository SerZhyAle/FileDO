' The Job Catalogue (SP-0006 section 12 / R2).
' Encapsulates the job definitions, parameter rules, and consequences for the UI Shell.
Public Class JobDefinition
    Public Property Id As String
    Public Property GroupKey As String
    Public Property LabelKey As String
    Public Property PurposeKey As String
    Public Property TargetKind As TargetType ' Drive, Folder, File, SourceAndTarget, None
    Public Property IsDestructive As Boolean = False
    Public Property NeedsElevation As Boolean = False
    Public Property DefaultVerb As String
    Public Property Reversibility As String ' reversible, partly reversible, permanent

    Public Enum TargetType
        Drive
        Folder
        File
        SourceAndTarget
        None
    End Enum
End Class

Public Module JobCatalogue

    Private ReadOnly jobs As New Dictionary(Of String, JobDefinition)()

    Sub New()
        ' Check group
        Register(New JobDefinition With {
            .Id = "rail_job_capacity",
            .GroupKey = "rail_group_check",
            .LabelKey = "rail_job_capacity",
            .PurposeKey = "purpose_job_capacity",
            .TargetKind = JobDefinition.TargetType.Drive,
            .DefaultVerb = "test",
            .Reversibility = "reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_speed",
            .GroupKey = "rail_group_check",
            .LabelKey = "rail_job_speed",
            .PurposeKey = "purpose_job_speed",
            .TargetKind = JobDefinition.TargetType.Drive,
            .DefaultVerb = "speed",
            .Reversibility = "reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_info",
            .GroupKey = "rail_group_check",
            .LabelKey = "rail_job_info",
            .PurposeKey = "purpose_job_info",
            .TargetKind = JobDefinition.TargetType.Drive,
            .DefaultVerb = "info",
            .Reversibility = "reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_damaged",
            .GroupKey = "rail_group_check",
            .LabelKey = "rail_job_damaged",
            .PurposeKey = "purpose_job_damaged",
            .TargetKind = JobDefinition.TargetType.Folder,
            .DefaultVerb = "check",
            .Reversibility = "reversible"
        })

        ' Probe and recover are the raw-I/O pair of the CLI (command_handlers.go): probe writes
        ' straight to the volume and can leave it unreadable, and recover is what puts it back.
        ' Both need Administrator, and both are destructive - which is why they were not worth
        ' leaving to the expert page alone, where nothing says either of those things.
        Register(New JobDefinition With {
            .Id = "rail_job_probe",
            .GroupKey = "rail_group_check",
            .LabelKey = "rail_job_probe",
            .PurposeKey = "purpose_job_probe",
            .TargetKind = JobDefinition.TargetType.Drive,
            .DefaultVerb = "probe",
            .IsDestructive = True,
            .NeedsElevation = True,
            .Reversibility = "permanent"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_recover",
            .GroupKey = "rail_group_check",
            .LabelKey = "rail_job_recover",
            .PurposeKey = "purpose_job_recover",
            .TargetKind = JobDefinition.TargetType.Drive,
            .DefaultVerb = "recover",
            .IsDestructive = True,
            .NeedsElevation = True,
            .Reversibility = "permanent"
        })

        ' Tidy group
        '
        ' Finding duplicates changes nothing, so the job is reversible. Delete and Move are answers
        ' on its page, and the page turns the badge, the accent and a typed word on when one is
        ' chosen (SP-0029 GUI-03), the way the Protect pages do for their dispositions.
        Register(New JobDefinition With {
            .Id = "rail_job_duplicates",
            .GroupKey = "rail_group_tidy",
            .LabelKey = "rail_job_duplicates",
            .PurposeKey = "purpose_job_duplicates",
            .TargetKind = JobDefinition.TargetType.Folder,
            .DefaultVerb = "cd",
            .Reversibility = "reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_compare",
            .GroupKey = "rail_group_tidy",
            .LabelKey = "rail_job_compare",
            .PurposeKey = "purpose_job_compare",
            .TargetKind = JobDefinition.TargetType.SourceAndTarget,
            .DefaultVerb = "compare",
            .Reversibility = "reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_clean",
            .GroupKey = "rail_group_tidy",
            .LabelKey = "rail_job_clean",
            .PurposeKey = "purpose_job_clean",
            .TargetKind = JobDefinition.TargetType.Drive,
            .DefaultVerb = "clean",
            .IsDestructive = True,
            .Reversibility = "permanent"
        })

        ' Move group
        Register(New JobDefinition With {
            .Id = "rail_job_copy",
            .GroupKey = "rail_group_move",
            .LabelKey = "rail_job_copy",
            .PurposeKey = "purpose_job_copy",
            .TargetKind = JobDefinition.TargetType.SourceAndTarget,
            .DefaultVerb = "copy",
            .Reversibility = "reversible"
        })

        ' Erase group
        Register(New JobDefinition With {
            .Id = "rail_job_fill",
            .GroupKey = "rail_group_erase",
            .LabelKey = "rail_job_fill",
            .PurposeKey = "purpose_job_fill",
            .TargetKind = JobDefinition.TargetType.Drive,
            .DefaultVerb = "fill",
            .IsDestructive = True,
            .Reversibility = "partly reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_wipe",
            .GroupKey = "rail_group_erase",
            .LabelKey = "rail_job_wipe",
            .PurposeKey = "purpose_job_wipe",
            .TargetKind = JobDefinition.TargetType.Folder,
            .DefaultVerb = "wipe",
            .IsDestructive = True,
            .Reversibility = "permanent"
        })

        ' Protect group - the secret files of SP-0005, one job per verb.
        '
        ' None of the three is marked destructive, and that is not an
        ' oversight: what each one does by default leaves every byte the user
        ' had. Secure keeps the original (Q9), unsecure writes a new file
        ' beside the container, and a reveal writes a copy and takes it away
        ' again. The destructive shapes are dispositions inside the page -
        ' "delete the original", "overwrite it" - and the page turns the
        ' badge, the accent and the typed confirmation on when one is chosen,
        ' which is the honest place for them: the job is not destructive, the
        ' answer to one of its questions is.
        Register(New JobDefinition With {
            .Id = "rail_job_secure",
            .GroupKey = "rail_group_protect",
            .LabelKey = "rail_job_secure",
            .PurposeKey = "purpose_job_secure",
            .TargetKind = JobDefinition.TargetType.File,
            .DefaultVerb = "secure",
            .Reversibility = "reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_unsecure",
            .GroupKey = "rail_group_protect",
            .LabelKey = "rail_job_unsecure",
            .PurposeKey = "purpose_job_unsecure",
            .TargetKind = JobDefinition.TargetType.File,
            .DefaultVerb = "unsecure",
            .Reversibility = "reversible"
        })

        Register(New JobDefinition With {
            .Id = "rail_job_reveal",
            .GroupKey = "rail_group_protect",
            .LabelKey = "rail_job_reveal",
            .PurposeKey = "purpose_job_reveal",
            .TargetKind = JobDefinition.TargetType.File,
            .DefaultVerb = "reveal",
            .Reversibility = "reversible"
        })
    End Sub

    Private Sub Register(job As JobDefinition)
        jobs(job.Id) = job
    End Sub

    Public Function GetJob(id As String) As JobDefinition
        If jobs.ContainsKey(id) Then Return jobs(id)
        Return Nothing
    End Function

    Public Function GetAllJobs() As IEnumerable(Of JobDefinition)
        Return jobs.Values
    End Function

End Module

' The CLI's own rule words, in one place because two pages ask for them.
'
' Each combo in the shell shows the token that will appear on the command line, and the line under
' it explains that token in the user's language. Keeping the tokens and the explanation keys side
' by side here is what stops the job page and the expert builder from offering different subsets
' of the same CLI - which is exactly how the shell lost the duplicate and compare rules the legacy
' builder already had.
Public Module CliRules

    ' `cd` - which copy of a duplicate group is treated as the original (fileduplicates.ParseArguments).
    Public ReadOnly DupRuleTokens As String() = {"", "old", "new", "abc", "xyz"}
    Public ReadOnly DupRuleKeys As String() = {"cdrule_default", "cdrule_old", "cdrule_new", "cdrule_abc", "cdrule_xyz"}

    ' `compare` - which side of a matched pair is deleted, if any (compare.go).
    Public ReadOnly CmpRuleTokens As String() = {
        "", "del source", "del target", "del old", "del new", "del small", "del big",
        "del old target", "del new source", "del small source", "del big target"
    }
    Public ReadOnly CmpRuleKeys As String() = {
        "cmprule_none", "cmprule_source", "cmprule_target", "cmprule_old", "cmprule_new",
        "cmprule_small", "cmprule_big", "cmprule_old_target", "cmprule_new_source",
        "cmprule_small_source", "cmprule_big_target"
    }

    ' Copy is seven verbs in the CLI, not one: the automatic one and the six that state the
    ' strategy outright (main.go). Each has an op_<verb> line of its own.
    Public ReadOnly CopyVerbs As String() = {
        "copy", "fastcopy", "synccopy", "balanced", "maxcopy", "smartcopy", "safecopy"
    }

End Module
