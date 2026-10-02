# Desktop Bridge optional tests are informational; required tests determine certification.
# https://learn.microsoft.com/windows/uwp/debug-test-perf/windows-desktop-bridge-app-tests
function Get-WackVerdict([xml]$Report) {
    $overall = $Report.DocumentElement.GetAttribute('OVERALL_RESULT')
    $tests = @($Report.SelectNodes('//TEST') | ForEach-Object {
        $result = $_.SelectSingleNode('RESULT')
        [pscustomobject]@{
            Name = $_.GetAttribute('NAME')
            Optional = ($_.GetAttribute('OPTIONAL') -ceq 'TRUE')
            Result = if ($result) { $result.InnerText.Trim() } else { '(none)' }
            Messages = @($_.SelectNodes('MESSAGES/MESSAGE') | ForEach-Object { $_.GetAttribute('TEXT') })
        }
    })
    $required = @($tests | Where-Object { -not $_.Optional })
    $requiredFailures = @($required | Where-Object { $_.Result -cne 'PASS' })
    $invalidResults = @($tests | Where-Object { $_.Result -cnotin @('PASS','FAIL','WARNING') })
    [pscustomobject]@{
        Overall = $overall
        Tests = $tests
        RequiredFailures = $requiredFailures
        Advisories = @($tests | Where-Object { $_.Optional -and $_.Result -cne 'PASS' })
        Passed = ($required.Count -gt 0 -and $requiredFailures.Count -eq 0 -and $invalidResults.Count -eq 0 -and $overall -cin @('PASS','WARNING'))
    }
}
