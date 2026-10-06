param(
    [string]$Profile = "realistic",
    [int[]]$Rps,
    [string]$Duration,
    [switch]$Reseed,
    [switch]$Warm,
    [switch]$NoUp,
    [switch]$FindMax,
    [int]$MaxRps = 2000,
    [int]$Step = 10,
    [double]$CheckP95Ms,
    [string]$Ramp,
    [int]$CacheTtlMs = -1,
    [int]$QueryCacheTtlMs = -1,
    [long]$BloomExpected = -1
)

$ErrorActionPreference = "Continue"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$expectedTuples = 4573820
$prom = "http://localhost:9090"

function Get-Seconds([string]$d) {
    $total = 0
    foreach ($m in [regex]::Matches($d, '(\d+)(h|m|s)')) {
        $n = [int]$m.Groups[1].Value
        switch ($m.Groups[2].Value) { 'h' { $total += $n * 3600 } 'm' { $total += $n * 60 } 's' { $total += $n } }
    }
    $total
}

function Invoke-Prom([string]$expr, [long]$at) {
    $u = "$prom/api/v1/query?time=$at&query=$([uri]::EscapeDataString($expr))"
    $r = (Invoke-WebRequest -UseBasicParsing $u).Content | ConvertFrom-Json
    $r.data.result
}

function Get-PromValue([string]$expr, [long]$at) {
    $r = Invoke-Prom $expr $at | Select-Object -First 1
    if (-not $r) { return $null }
    $v = [double]$r.value[1]
    if ([double]::IsNaN($v)) { return $null }
    $v
}

function Invoke-Psql([string]$sql) {
    (docker exec margit-postgres-1 psql -U margit -d margit -tAc $sql | Out-String).Trim()
}

function Wait-Healthy([string]$container) {
    for ($i = 0; $i -lt 180; $i++) {
        if ((docker inspect -f '{{.State.Health.Status}}' $container) -eq 'healthy') { return }
        Start-Sleep 1
    }
    throw "$container not healthy"
}

function Fmt($v, [string]$f = "N1") { if ($null -eq $v) { "-" } else { ([double]$v).ToString($f) } }

$profilePath = if (Test-Path $Profile) { (Resolve-Path $Profile).Path } else { Join-Path $root "loadtest\profiles\$Profile.json" }
if (-not (Test-Path $profilePath)) { throw "profile not found: $profilePath" }
$cfg = Get-Content -Raw $profilePath | ConvertFrom-Json
$profileName = $cfg.name
$profileFile = Split-Path -Leaf $profilePath
if (-not (Test-Path (Join-Path $root "loadtest\profiles\$profileFile"))) { throw "profile must live in loadtest\profiles (mounted into k6)" }
$rates = if ($Rps) { $Rps } else { @([int]$cfg.rps) }
$hold = if ($Duration) { $Duration } else { $cfg.duration }
$holdSec = Get-Seconds $hold
$ramp = if ($Ramp) { $Ramp } else { $cfg.ramp }
$cold = [bool]$cfg.cold_start -and -not $Warm

$r = $cfg.resources
$env:MARGIT_CPUS = "$($r.margit.cpus)"
$env:MARGIT_MEM = "$($r.margit.memory)"
$env:MARGIT_GOMAXPROCS = "$($r.margit.gomaxprocs)"
$env:MARGIT_GOMEMLIMIT = "$($r.margit.gomemlimit)"
$cacheTtl = if ($CacheTtlMs -ge 0) { $CacheTtlMs } elseif ($r.margit.cache_ttl_ms) { [int]$r.margit.cache_ttl_ms } else { 0 }
$env:MARGIT_CACHE_TTL_MS = "$cacheTtl"
$env:MARGIT_CACHE_MAX_ENTRIES = if ($r.margit.cache_max_entries) { "$($r.margit.cache_max_entries)" } else { "1000000" }
$queryCacheTtl = if ($QueryCacheTtlMs -ge 0) { $QueryCacheTtlMs } elseif ($r.margit.query_cache_ttl_ms) { [int]$r.margit.query_cache_ttl_ms } else { 0 }
$env:MARGIT_QUERY_CACHE_TTL_MS = "$queryCacheTtl"
$env:MARGIT_QUERY_CACHE_MAX_ENTRIES = if ($r.margit.query_cache_max_entries) { "$($r.margit.query_cache_max_entries)" } else { "500000" }
$tag = $profileName
if ($cacheTtl -gt 0) { $tag += "-cache$($cacheTtl)ms" }
if ($queryCacheTtl -gt 0) { $tag += "-qcache$($queryCacheTtl)ms" }
$bloom = if ($BloomExpected -ge 0) { $BloomExpected } elseif ($null -ne $r.margit.bloom_expected) { [long]$r.margit.bloom_expected } else { 12000000 }
$env:MARGIT_BLOOM_EXPECTED = "$bloom"
if ($bloom -eq 0) { $tag += "-nobloom" }
$env:PG_SHARED_BUFFERS = "$($r.postgres.shared_buffers)"
$env:PG_EFFECTIVE_CACHE_SIZE = "$($r.postgres.effective_cache_size)"
$env:PG_MEM_LIMIT = "$($r.postgres.memory)"

if (-not $NoUp) {
    Write-Host "==> compose up (margit $($env:MARGIT_CPUS) CPU / $($env:MARGIT_MEM), response cache ttl $cacheTtl ms, query cache ttl $queryCacheTtl ms, bloom expected $bloom, postgres buffers $($env:PG_SHARED_BUFFERS) / $($env:PG_MEM_LIMIT))"
    docker compose up -d --wait 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "docker compose up failed" }
}
Wait-Healthy margit-margit-1

$count = [long](Invoke-Psql "select count(*) from relation_tuples")
$hasSchema = (Invoke-Psql "select count(*) from namespaces where name in ('user','group','role','org','folder','file')") -eq '6'
Write-Host "==> dataset: $count tuples (expected >= $expectedTuples), schema present: $hasSchema"
if ($Reseed -or $count -lt $expectedTuples -or -not $hasSchema) {
    Write-Host "==> seeding"
    docker compose run --rm k6 run --quiet /scripts/drive/seed.js
    if ($LASTEXITCODE -ne 0) { throw "seed failed" }
    $count = [long](Invoke-Psql "select count(*) from relation_tuples")
    Write-Host "==> dataset: $count tuples"
}

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$results = Join-Path $root "loadtest\results"
New-Item -ItemType Directory -Force $results | Out-Null
$commit = (git rev-parse --short HEAD 2>$null)
$script:summary = @()
$script:failed = $false

function Invoke-Run([int]$rate) {
    $runId = "$tag-$rate-$stamp"
    Write-Host "==> run $runId (rps $rate, ramp $ramp, hold $hold, cold $cold)"
    if ($cold) {
        docker compose restart postgres 2>&1 | Out-Null
        docker run --rm --privileged alpine sh -c "sync; echo 3 > /proc/sys/vm/drop_caches" | Out-Null
        Wait-Healthy margit-postgres-1
    }
    Invoke-Psql "select pg_stat_reset(); select pg_stat_statements_reset();" | Out-Null

    $k6Args = @("compose", "run", "--rm",
        "-e", "PROFILE=/scripts/profiles/$profileFile", "-e", "RPS=$rate", "-e", "RAMP=$ramp", "-e", "DURATION=$hold", "-e", "RUN_ID=$runId",
        "k6", "run", "--quiet", "/scripts/drive/traffic.js")
    $out = & docker @k6Args 2>&1 | ForEach-Object { "$_" }
    $k6Exit = $LASTEXITCODE
    $end = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $out | Set-Content -Encoding utf8 (Join-Path $results "$runId.txt")

    $at = $end - 6
    $w = "[$($holdSec)s]"
    $sw = "[$($holdSec)s:5s]"
    $cpuExpr = { param($svc) "sum(rate(container_cpu_usage_seconds_total{service=`"$svc`"}[15s]))" }
    $res = [ordered]@{
        margit_cpu_avg   = Get-PromValue "avg_over_time(($(& $cpuExpr 'margit'))$sw)" $at
        margit_cpu_max   = Get-PromValue "max_over_time(($(& $cpuExpr 'margit'))$sw)" $at
        postgres_cpu_avg = Get-PromValue "avg_over_time(($(& $cpuExpr 'postgres'))$sw)" $at
        postgres_cpu_max = Get-PromValue "max_over_time(($(& $cpuExpr 'postgres'))$sw)" $at
        margit_mem_max   = Get-PromValue "max_over_time(sum(container_memory_working_set_bytes{service=`"margit`"})$sw)" $at
        postgres_mem_max = Get-PromValue "max_over_time(sum(container_memory_working_set_bytes{service=`"postgres`"})$sw)" $at
        pg_conn_max      = Get-PromValue "max_over_time(sum(pg_stat_database_numbackends{datname=`"margit`"})$sw)" $at
        pg_tps           = Get-PromValue "sum(rate(pg_stat_database_xact_commit{datname=`"margit`"}$w))" $at
        pg_statements_ps = Get-PromValue "sum(rate(margit_statements_calls$w))" $at
        pg_stmt_mean_us  = Get-PromValue "sum(rate(margit_statements_exec_seconds$w)) / sum(rate(margit_statements_calls$w)) * 1e6" $at
        server_rps       = Get-PromValue "sum(rate(margit_http_requests_total{route=~`"/v1/.*`"}$w))" $at
        cache_hit_ratio  = Get-PromValue "sum(rate(margit_cache_hits_total{cache=`"response`"}$w)) / (sum(rate(margit_cache_hits_total{cache=`"response`"}$w)) + sum(rate(margit_cache_misses_total{cache=`"response`"}$w)))" $at
        qcache_hit_ratio = Get-PromValue "sum(rate(margit_cache_hits_total{cache=`"query`"}$w)) / (sum(rate(margit_cache_hits_total{cache=`"query`"}$w)) + sum(rate(margit_cache_misses_total{cache=`"query`"}$w)))" $at
        server_p95_ms    = Get-PromValue "histogram_quantile(0.95, sum by (le) (rate(margit_http_request_duration_seconds_bucket{route=~`"/v1/.*`"}$w))) * 1000" $at
    }
    $io = Invoke-Psql "select blks_hit || ',' || blks_read from pg_stat_database where datname='margit'"
    $hit, $read = $io.Split(',') | ForEach-Object { [double]$_ }
    $res.pg_hit_ratio = if ($hit + $read -gt 0) { $hit / ($hit + $read) } else { $null }
    $res.pg_blocks_read = $read

    $k6 = Get-Content -Raw (Join-Path $results "$runId.k6.json") | ConvertFrom-Json
    $pass = ($k6.breached.Count -eq 0) -and ($k6Exit -eq 0)
    if (-not $pass) { $failed = $true }
    $report = [ordered]@{
        run = $runId; profile = $profileName; commit = $commit; rps_target = $rate; hold = $hold; cold_start = $cold
        cache_ttl_ms = $cacheTtl; query_cache_ttl_ms = $queryCacheTtl; bloom_expected = $bloom; resources = $cfg.resources; dataset_tuples = $count; result = if ($pass) { "PASS" } else { "FAIL" }
        breached = @($k6.breached); k6_exit = $k6Exit; ops = $k6.ops; total = $k6.total; resources_observed = $res
    }
    $report | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 (Join-Path $results "$runId.json")

    $md = @(
        "# $runId", "",
        "- profile: ``$profileName`` ($($cfg.description))",
        "- commit: ``$commit``, dataset: $count tuples, cold start: $cold",
        "- target: $rate it/s, ramp $ramp, hold $hold",
        "- margit: $($r.margit.cpus) CPU / $($r.margit.memory), response cache ttl $cacheTtl ms, query cache ttl $queryCacheTtl ms, bloom expected $bloom; postgres: shared_buffers $($r.postgres.shared_buffers), limit $($r.postgres.memory)",
        "- result: **$($report.result)**$(if (-not $pass) { ' (' + ($k6.breached -join ', ') + ')' })", "",
        "| op | n | p95 ms | p99 ms | SLO |", "|---|---|---|---|---|"
    )
    foreach ($p in $k6.ops.PSObject.Properties) {
        $md += "| $($p.Name) | $($p.Value.n) | $(Fmt $p.Value.p95) | $(Fmt $p.Value.p99) | $(if ($p.Value.slo) { ($p.Value.slo | ConvertTo-Json -Compress) } else { '-' }) |"
    }
    $t = $k6.total
    $md += "", "Total: $($t.reqs) requests, $(Fmt $t.rate) req/s, p95 $(Fmt $t.p95) ms, p99 $(Fmt $t.p99) ms, failed $(Fmt ($t.failed * 100) 'N2')%, dropped $($t.dropped), correct $(Fmt ($t.correct * 100) 'N2')%", ""
    $md += "| resource (hold window) | value |", "|---|---|"
    $md += "| margit CPU avg / max (cores) | $(Fmt $res.margit_cpu_avg 'N2') / $(Fmt $res.margit_cpu_max 'N2') |"
    $md += "| postgres CPU avg / max (cores) | $(Fmt $res.postgres_cpu_avg 'N2') / $(Fmt $res.postgres_cpu_max 'N2') |"
    $md += "| margit / postgres memory max (MiB) | $(Fmt ($res.margit_mem_max / 1MB) 'N0') / $(Fmt ($res.postgres_mem_max / 1MB) 'N0') |"
    $md += "| postgres connections max | $(Fmt $res.pg_conn_max 'N0') |"
    $md += "| postgres TPS / statements per s | $(Fmt $res.pg_tps 'N0') / $(Fmt $res.pg_statements_ps 'N0') |"
    $md += "| statement mean exec (us) | $(Fmt $res.pg_stmt_mean_us) |"
    $md += "| buffer cache hit ratio / blocks read | $(Fmt ($res.pg_hit_ratio * 100) 'N2')% / $($res.pg_blocks_read) |"
    $md += "| response / query cache hit ratio | $(Fmt ($res.cache_hit_ratio * 100) 'N2')% / $(Fmt ($res.qcache_hit_ratio * 100) 'N2')% |"
    $md += "| server req/s / p95 ms | $(Fmt $res.server_rps) / $(Fmt $res.server_p95_ms) |"
    $md += "", '```', (($out | Where-Object { $_ -notmatch 'level=' }) -join "`n").Trim(), '```'
    $md -join "`n" | Set-Content -Encoding utf8 (Join-Path $results "$runId.md")

    $row = [pscustomobject]@{
        rps = $rate; req_s = Fmt $res.server_rps 'N0'; result = $report.result
        check_p95 = Fmt $k6.ops.check.p95; lookup_p95 = Fmt $k6.ops.lookup.p95; expand_p95 = Fmt $k6.ops.expand.p95; write_p95 = Fmt $k6.ops.write.p95
        p99 = Fmt $t.p99; dropped = $t.dropped; margit_cpu = Fmt $res.margit_cpu_avg 'N2'; pg_cpu = Fmt $res.postgres_cpu_avg 'N2'
        hit = Fmt ($res.pg_hit_ratio * 100) 'N2'; cache_hit = Fmt ($res.cache_hit_ratio * 100) 'N1'
        qcache_hit = Fmt ($res.qcache_hit_ratio * 100) 'N1'
    }
    $healthy = ($t.failed -le 0.01) -and ($t.dropped -le [math]::Max(5, $t.reqs * 0.005)) -and ($t.correct -ge 0.999)
    $row | Add-Member NoteProperty check_p95_ms $k6.ops.check.p95
    $row | Add-Member NoteProperty healthy $healthy
    $script:summary += $row
    Write-Host ($row | Format-Table -AutoSize -Property $cols | Out-String).TrimEnd()
    $row
}

$cols = "rps", "req_s", "result", "check_p95", "lookup_p95", "expand_p95", "write_p95", "p99", "dropped", "margit_cpu", "pg_cpu", "hit", "cache_hit", "qcache_hit"
$maxLine = ""
if ($FindMax) {
    $limit = if ($CheckP95Ms) { $CheckP95Ms } elseif ($cfg.slo.check.p95_ms) { [double]$cfg.slo.check.p95_ms } else { 50 }
    $ok = { param($x) $x.healthy -and ($null -ne $x.check_p95_ms) -and ($x.check_p95_ms -lt $limit) }
    $lo = 0; $hi = 0; $best = $null; $rate = $rates[0]
    while ($true) {
        $row = Invoke-Run $rate
        if (& $ok $row) {
            $lo = $rate; $best = $row
            if ($hi -or $rate -ge $MaxRps) { break }
            $rate = [math]::Min($MaxRps, $rate * 2)
        } else {
            $hi = $rate
            if ($lo -or $rate -le $Step) { break }
            $rate = [int]($rate / 2)
        }
        Start-Sleep 10
    }
    while ($lo -and $hi -and ($hi - $lo) -gt [math]::Max($Step, [int]($lo * 0.05))) {
        Start-Sleep 10
        $mid = [int](($lo + $hi) / 2)
        $row = Invoke-Run $mid
        if (& $ok $row) { $lo = $mid; $best = $row } else { $hi = $mid }
    }
    $script:failed = -not $best
    $next = if ($hi) { "$hi" } else { "none up to $MaxRps" }
    $maxLine = if ($best) { "max $lo it/s ($($best.req_s) req/s at the server), check p95 $($best.check_p95) ms < $limit ms; first failing rate $next" } else { "no rate >= $Step met check p95 < $limit ms" }
    Write-Host "`n==> $tag $maxLine"
} else {
    foreach ($rate in $rates) { Invoke-Run $rate | Out-Null }
}

$title = if ($FindMax) { "max search" } else { "sweep" }
$sumMd = @("# $tag $title $stamp", "", $maxLine, "", "| " + ($cols -join " | ") + " |", "|" + ("---|" * $cols.Count))
foreach ($s in $summary) { $sumMd += "| " + (($cols | ForEach-Object { $s.$_ }) -join " | ") + " |" }
$sumPath = Join-Path $results "$tag-$stamp-summary.md"
$sumMd -join "`n" | Set-Content -Encoding utf8 $sumPath
Write-Host "`n==> summary"
Write-Host ($summary | Format-Table -AutoSize -Property $cols | Out-String).TrimEnd()
Write-Host "reports: $results ($profileName-*-$stamp.md, $profileName-$stamp-summary.md)"
if ($failed) { exit 1 }
