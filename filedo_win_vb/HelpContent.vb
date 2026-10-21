' The help pages' content (SP-0168; contract CONTEXT-HELP 0.1 rules 8, 9, 11, 13): which parts a page
' explains and in what order, as keys of the one string source.
'
' A page is written once here and read by the generator (HelpPages.vb), which turns it into an HTML
' file per language, and by the resolver (Help.vb), which asks whether an element has an entry so that F1
' on a focused part lands on it. The words are never here: every name, line and explanation is a key
' of Localization, in all five languages, so a renamed button is renamed in its help by the same edit
' (rule 13). A name and a one-line summary reuse the UI's own string wherever the UI has one.
'
' An element names the control it explains by the Name the view gives it ("help:<id>", or
' "setting:<id>" for a row of Settings), which is also how the capture finds its bounds for the
' numbered marker (rule 14) and how F1 finds the element a focus is in (Help.ElementOf).
Friend Class HelpElementDef
    Friend ReadOnly Id As String
    Friend ReadOnly NameKey As String
    Friend ReadOnly OneKey As String
    Friend ReadOnly FullKey As String
    ' "" when the element has no such line.
    Friend ReadOnly NextKey As String
    Friend ReadOnly UndoKey As String
    ' The shortcut as the UI spells it ("Ctrl+O"); "" when there is none. Not translated.
    Friend ReadOnly KeyText As String
    ' The Name of the control the marker points at.
    Friend ReadOnly Locator As String
    ' "keys" or "legend": a table the generator adds from the code that owns it (Disk Manager).
    Friend ReadOnly Extra As String

    Friend Sub New(id As String, nameKey As String, oneKey As String, fullKey As String, nextKey As String,
                   undoKey As String, keyText As String, locator As String, extra As String)
        Me.Id = id
        Me.NameKey = nameKey
        Me.OneKey = oneKey
        Me.FullKey = fullKey
        Me.NextKey = nextKey
        Me.UndoKey = undoKey
        Me.KeyText = keyText
        Me.Locator = locator
        Me.Extra = extra
    End Sub
End Class

' One entry of Related (rule 11). Kind is "window" (another help page, Target a help id), "site" (a
' page of the product's site, Target the path under the site), "sibling" (a product that shares the
' function, Target a full address) or "portal" (the author's hub, Target a full address).
Friend Class HelpLinkDef
    Friend ReadOnly Kind As String
    Friend ReadOnly Target As String
    Friend ReadOnly TitleKey As String
    Friend ReadOnly NoteKey As String

    Friend Sub New(kind As String, target As String, titleKey As String, noteKey As String)
        Me.Kind = kind
        Me.Target = target
        Me.TitleKey = titleKey
        Me.NoteKey = noteKey
    End Sub
End Class

Friend Class HelpPageDef
    Friend ReadOnly Id As String
    Friend ReadOnly Prefix As String
    Friend ReadOnly SurfaceId As String
    Friend ReadOnly TitleKey As String
    Friend ReadOnly ForKey As String
    ' Keys of the steps of "In this order", at most seven; a step names a part as [element-id].
    Friend ReadOnly StepKeys As String()
    ' For a surface with no order (a settings page) the sentence that says so, in place of the steps.
    Friend ReadOnly NoOrderKey As String
    Friend ReadOnly Elements As HelpElementDef()
    Friend ReadOnly Related As HelpLinkDef()
    ' How the generator takes the picture: "shell|<rail key>|<target>", "disk-manager".
    Friend ReadOnly Capture As String

    Friend Sub New(id As String, prefix As String, surfaceId As String, titleKey As String, forKey As String,
                   stepKeys As String(), noOrderKey As String, elements As HelpElementDef(), related As HelpLinkDef(),
                   capture As String)
        Me.Id = id
        Me.Prefix = prefix
        Me.SurfaceId = surfaceId
        Me.TitleKey = titleKey
        Me.ForKey = forKey
        Me.StepKeys = stepKeys
        Me.NoOrderKey = noOrderKey
        Me.Elements = elements
        Me.Related = related
        Me.Capture = capture
    End Sub

    Friend Function ElementById(elementId As String) As HelpElementDef
        Return Elements.FirstOrDefault(Function(e) e.Id = elementId)
    End Function
End Class

Friend Module HelpContent

    ' The element every page ends with, whose words are the same on all of them.
    Private Function HelpButtonElement() As HelpElementDef
        Return New HelpElementDef("help", "vd_mgr_name_help", "help_el_help_one", "help_el_help_full", "help_el_help_next",
                                  "", "F1", "help:help", "")
    End Function

    ' An element whose words follow the naming rule: help_<page>_<id>_one / _full / _next / _undo, and
    ' _name when the UI has no string for it. parts holds "n" for a next line and "u" for an undo line.
    Private Function E(page As String, id As String, Optional nameKey As String = "", Optional parts As String = "",
                       Optional oneKey As String = "", Optional keyText As String = "", Optional locator As String = Nothing,
                       Optional extra As String = "") As HelpElementDef
        Dim stem = "help_" & page & "_" & id.Replace("-", "_")
        Return New HelpElementDef(id,
                                  If(nameKey = "", stem & "_name", nameKey),
                                  If(oneKey = "", stem & "_one", oneKey),
                                  stem & "_full",
                                  If(parts.Contains("n"), stem & "_next", ""),
                                  If(parts.Contains("u"), stem & "_undo", ""),
                                  keyText,
                                  If(locator, "help:" & id),
                                  extra)
    End Function

    Private Function Win(helpId As String, titleKey As String, noteKey As String) As HelpLinkDef
        Return New HelpLinkDef("window", helpId, titleKey, noteKey)
    End Function

    Private Function Site(path As String, titleKey As String, noteKey As String) As HelpLinkDef
        Return New HelpLinkDef("site", path, titleKey, noteKey)
    End Function

    Private Function Steps(prefix As String, count As Integer) As String()
        Dim keys(count - 1) As String
        For i = 0 To count - 1
            keys(i) = "help_" & prefix & "_step" & (i + 1).ToString()
        Next
        Return keys
    End Function

    Friend ReadOnly Pages As HelpPageDef() = {
        New HelpPageDef("job-page", "jp", "", "help_jp_title", "help_jp_for", Steps("jp", 6), "",
            {E("jp", "rail", parts:="nu"),
             E("jp", "target", "shell_step2", "nu"),
             E("jp", "options", "shell_step3", "nu"),
             E("jp", "plan", "shell_step4", "nu"),
             E("jp", "command", parts:="nu"),
             E("jp", "copy", "shell_btn_copy_cmd", locator:="help:copy"),
             E("jp", "open", "shell_btn_open_in_cmd", "n"),
             E("jp", "run", parts:="nu"),
             E("jp", "progress"),
             E("jp", "stop", "shell_btn_stop", "nu"),
             E("jp", "result", parts:="nu"),
             E("jp", "output", "shell_btn_show_output"),
             HelpButtonElement()},
            {Win("command", "rail_job_command", "help_rel_command_note"),
             Win("history", "rail_job_history", "help_rel_history_note"),
             Win("settings", "rail_job_settings", "help_rel_settings_note"),
             Site("guides/gui-command-builder.html", "help_rel_guide_gui_title", "help_rel_guide_gui_note"),
             Site("guides/check-storage.html", "help_rel_guide_check_title", "help_rel_guide_whole_note"),
             Site("guides/files-and-copies.html", "help_rel_guide_copy_title", "help_rel_guide_whole_note")},
            "shell|rail_job_info|D:\Photos"),
        New HelpPageDef("command", "cm", "shell.command", "rail_job_command", "help_cm_for", Steps("cm", 5), "",
            {E("cm", "operation", "ui_operation", "nu"),
             E("cm", "target", "shell_lbl_target", "nu"),
             E("cm", "parameter", "shell_lbl_params"),
             E("cm", "flags", "ui_flags", "nu"),
             E("cm", "commandline", "shell_lbl_command_editable", "u"),
             E("cm", "run", "shell_btn_run_cmd", "nu"),
             E("cm", "stop", "shell_btn_stop", "u"),
             E("cm", "copy", "shell_btn_copy_cmd"),
             E("cm", "shortcut", "shortcut_create", "nu"),
             E("cm", "output"),
             HelpButtonElement()},
            {Win("job-page", "help_jp_title", "help_rel_jobpage_note"),
             Win("history", "rail_job_history", "help_rel_history_note"),
             Site("guides/gui-command-builder.html", "help_rel_guide_gui_title", "help_rel_guide_gui_note"),
             Site("guides/glossary.html", "help_rel_glossary_title", "help_rel_glossary_note")},
            "shell|rail_job_command|"),
        New HelpPageDef("settings", "st", "shell.settings", "rail_job_settings", "help_st_for", {}, "help_st_noorder",
            {E("st", "group-appearance", "settings_appearance", oneKey:="help_st_group_appearance_one", locator:="settings-group:appearance:header"),
             E("st", "setting-theme", "shell_settings_theme", "nu", "settings_theme_hint", locator:="setting:theme"),
             E("st", "setting-language", "shell_settings_lang", "n", "shell_settings_lang_hint", locator:="setting:language"),
             E("st", "group-history", "shell_settings_history_title", oneKey:="help_st_group_history_one", locator:="settings-group:history:header"),
             E("st", "setting-history", "shell_settings_history", "u", "shell_settings_history_hint", locator:="setting:history"),
             E("st", "group-startup", "settings_startup", oneKey:="help_st_group_startup_one", locator:="settings-group:startup:header"),
             E("st", "setting-startup", "settings_startup", "n", "settings_startup_hint", locator:="setting:startup"),
             E("st", "setting-startup-apply", "settings_startup_apply", "u", "help_st_setting_startup_apply_one", locator:="setting:startup-apply"),
             E("st", "setting-minimize", "settings_minimize_tray", "u", "settings_minimize_hint", locator:="setting:minimize"),
             E("st", "setting-notification", "settings_notify", "u", "settings_notify_hint", locator:="setting:notification"),
             E("st", "group-windows", "settings_windows", oneKey:="help_st_group_windows_one", locator:="settings-group:windows:header"),
             E("st", "setting-autoplay", "settings_autoplay_disable", "nu", "help_st_setting_autoplay_one", locator:="setting:autoplay"),
             E("st", "setting-autoplay-system", "settings_autoplay_open", "n", "help_st_setting_autoplay_system_one", locator:="setting:autoplay-system"),
             E("st", "setting-reset", "settings_reset", "nu", "settings_reset_hint", locator:="setting:reset"),
             HelpButtonElement()},
            {Win("about", "rail_job_about", "help_rel_about_note"),
             Win("history", "rail_job_history", "help_rel_history_note"),
             Site("privacy.html", "help_rel_privacy_title", "help_rel_privacy_note")},
            "shell|rail_job_settings||1180x1500"),
        New HelpPageDef("about", "ab", "shell.about", "rail_job_about", "help_ab_for", Steps("ab", 3), "",
            {E("ab", "build", "about_build", "u"),
             E("ab", "author", "about_author"),
             E("ab", "links", "about_links", "n"),
             E("ab", "license", oneKey:="help_ab_license_one"),
             E("ab", "logs", "ui_send_logs", "nu"),
             HelpButtonElement()},
            {Win("settings", "rail_job_settings", "help_rel_settings_note"),
             Site("guides/support.html", "help_rel_support_title", "help_rel_support_note"),
             Site("privacy.html", "help_rel_privacy_title", "help_rel_privacy_note"),
             New HelpLinkDef("portal", "https://sza.od.ua", "about_portfolio", "help_rel_hub_note")},
            "shell|rail_job_about|"),
        New HelpPageDef("history", "hi", "shell.history", "rail_job_history", "help_hi_for", Steps("hi", 4), "",
            {E("hi", "state", oneKey:="help_hi_state_one"),
             E("hi", "list", "rail_job_history", "nu", "help_hi_list_one"),
             E("hi", "details", "shell_history_details", oneKey:="help_hi_details_one"),
             E("hi", "open", "shell_btn_open_report", "n"),
             E("hi", "folder", "shell_btn_show_reports_dir"),
             E("hi", "refresh", "shell_btn_refresh"),
             HelpButtonElement()},
            {Win("job-page", "help_jp_title", "help_rel_jobpage_note"),
             Win("command", "rail_job_command", "help_rel_command_note"),
             Win("settings", "rail_job_settings", "help_rel_settings_note")},
            "shell|rail_job_history|"),
        New HelpPageDef("disk-manager", "dm", "disk-manager", "vd_mgr_title", "help_dm_for", Steps("dm", 5), "",
            {E("dm", "new", "vd_mgr_btn_new", "nu", "vd_tip_new", "Ctrl+N"),
             E("dm", "add", "vd_mgr_btn_add", "u", "vd_mgr_btn_add_tip", "Ctrl+O"),
             E("dm", "mount", "vd_mgr_btn_mount", "nu", "vd_tip_mount", "Ctrl+M"),
             E("dm", "unmount", "vd_mgr_btn_unmount", "nu", "vd_tip_unmount", "Ctrl+U"),
             E("dm", "open", "vd_mgr_btn_open", oneKey:="vd_tip_open", keyText:="Ctrl+E"),
             E("dm", "save", "vd_mgr_btn_save", "u", "vd_tip_save", "Ctrl+S"),
             E("dm", "more", "vd_mgr_btn_more", "n", "vd_mgr_btn_more_tip"),
             E("dm", "refresh", "vd_mgr_btn_refresh", oneKey:="vd_tip_refresh", keyText:="F5"),
             E("dm", "settings", "rail_job_settings", "u"),
             E("dm", "main", "vd_mgr_btn_main", oneKey:="vd_tip_main", keyText:="Ctrl+Shift+O"),
             HelpButtonElement(),
             E("dm", "filter", "vd_mgr_filter_name", oneKey:="vd_tip_filter", keyText:="Ctrl+F"),
             E("dm", "list", parts:="u", extra:="legend"),
             E("dm", "detail", parts:="n"),
             E("dm", "strip"),
             E("dm", "keyboard", "vd_help_keys_title", extra:="keys", locator:="")},
            {Win("job-page", "help_jp_title", "help_rel_dm_jobs_note"),
             Win("settings", "rail_job_settings", "help_rel_settings_note"),
             Site("guides/virtual-disks.html", "help_rel_guide_vd_title", "help_rel_guide_whole_note"),
             Site("guides/fms-sharing.html", "help_rel_guide_fms_title", "help_rel_guide_fms_note"),
             New HelpLinkDef("sibling", Links.FMSInstall, "help_rel_fms_title", "help_rel_fms_note"),
             Site("guides/glossary.html", "help_rel_glossary_title", "help_rel_glossary_note")},
            "disk-manager")
    }

    Friend Function PageById(helpId As String) As HelpPageDef
        Return Pages.FirstOrDefault(Function(p) p.Id = helpId)
    End Function

    Friend Function AnchorOf(elementId As String) As String
        Return "e-" & elementId
    End Function

    Friend Function HasElement(helpId As String, elementId As String) As Boolean
        Dim p = PageById(helpId)
        Return p IsNot Nothing AndAlso p.ElementById(elementId) IsNot Nothing
    End Function
End Module
