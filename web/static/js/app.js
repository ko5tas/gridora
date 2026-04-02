// Gridora frontend — unified dashboard with SSE + Chart.js
const Gridora = (() => {
    const COLORS = {
        consumption: '#f97316',
        generation:  '#22c55e',
        export:      '#eab308',
        import:      '#ef4444',
        diverted:    '#3b82f6',
    };

    // Dark theme defaults for Chart.js
    Chart.defaults.color = '#9ca3af';
    Chart.defaults.borderColor = 'rgba(255,255,255,0.08)';

    let chart = null;
    let serial = '';
    let liveMode = true;      // Whether live gauges + auto-refresh are active
    let refreshTimer = null;
    let filterAfter = null;   // ISO string — discard data before this timestamp (for "Last 12h")

    function init(s) {
        serial = s;
        chart = createChart();
        startSSE();
        bindControls();

        // Restore view from URL or default to "Today"
        if (!restoreFromURL()) {
            selectRange('today');
        }

        // Handle browser back/forward
        window.addEventListener('popstate', () => restoreFromURL());
    }

    // ── SSE (always connected, gauges shown/hidden) ───────────

    function startSSE() {
        const url = '/api/v1/status/stream' + (serial ? '?serial=' + serial : '');
        const source = new EventSource(url);

        source.onmessage = (e) => updateGauges(JSON.parse(e.data));
        source.onerror = () => {
            document.getElementById('status-bar').textContent = 'Connection lost. Reconnecting...';
        };
        source.onopen = () => {
            document.getElementById('status-bar').textContent = 'Connected';
        };
    }

    function updateGauges(d) {
        const gridW = Math.round(d.grid_w);

        // Positive grid = importing, negative = exporting
        document.getElementById('grid-export-value').textContent = gridW < 0 ? Math.abs(gridW).toLocaleString() : '0';
        document.getElementById('grid-import-value').textContent = gridW > 0 ? gridW.toLocaleString() : '0';

        // Live consumption = generation + grid (grid is negative when exporting)
        const consumptionW = Math.round(d.generation_w + d.grid_w);
        document.getElementById('consumption-value').textContent = Math.max(0, consumptionW).toLocaleString();

        document.getElementById('gen-value').textContent = Math.round(d.generation_w).toLocaleString();
        document.getElementById('div-value').textContent = Math.round(d.diversion_w).toLocaleString();
        document.getElementById('voltage-value').textContent = d.voltage.toFixed(1);
        document.getElementById('mode-value').textContent = d.zappi_mode_name;
        document.getElementById('charge-value').textContent = d.charge_added_kwh.toFixed(1);
        document.getElementById('status-bar').textContent = 'Last update: ' + d.timestamp;
    }

    function setLiveMode(on) {
        liveMode = on;
        document.getElementById('live-gauges').style.display = on ? '' : 'none';
        document.getElementById('status-bar').style.display = on ? '' : 'none';

        // Auto-refresh chart every 5 min in live mode
        if (refreshTimer) clearInterval(refreshTimer);
        if (on) {
            refreshTimer = setInterval(loadChart, 5 * 60 * 1000);
        }
    }

    // ── Controls ──────────────────────────────────────────────

    function bindControls() {
        document.querySelectorAll('.quick-range button').forEach(btn => {
            btn.addEventListener('click', (e) => {
                document.querySelectorAll('.quick-range button').forEach(b => b.classList.remove('active'));
                e.target.classList.add('active');
                selectRange(e.target.dataset.range);
            });
        });

        document.getElementById('from-date').addEventListener('change', () => {
            clearActiveButton();
            filterAfter = null;
            setLiveMode(false);
            autoResolution();
            loadChart();
        });
        document.getElementById('to-date').addEventListener('change', () => {
            clearActiveButton();
            filterAfter = null;
            setLiveMode(false);
            autoResolution();
            loadChart();
        });
        document.getElementById('resolution').addEventListener('change', loadChart);
        document.getElementById('exclude-ev').addEventListener('change', loadChart);
    }

    function clearActiveButton() {
        document.querySelectorAll('.quick-range button').forEach(b => b.classList.remove('active'));
    }

    // Auto-select resolution based on date span to prevent browser overload
    function autoResolution() {
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        if (!from || !to) return;

        const days = (new Date(to) - new Date(from)) / (1000 * 60 * 60 * 24);
        let resolution;
        if (days <= 2) {
            resolution = 'minute';
        } else if (days <= 14) {
            resolution = 'hourly';
        } else {
            resolution = 'daily';
        }
        document.getElementById('resolution').value = resolution;
    }

    function selectRange(range) {
        const now = new Date();
        const today = isoDate(now);
        let from, to, resolution;

        switch (range) {
            case '12h': {
                const twelveAgo = new Date(now.getTime() - 12 * 60 * 60 * 1000);
                from = isoDate(twelveAgo);
                to = today;
                resolution = 'minute';
                filterAfter = twelveAgo.toISOString();
                setLiveMode(true);
                break;
            }
            case 'today':
                from = today;
                to = today;
                resolution = 'minute';
                filterAfter = null;
                setLiveMode(true);
                break;
            case 'yesterday': {
                const y = new Date(now);
                y.setDate(y.getDate() - 1);
                from = isoDate(y);
                to = isoDate(y);
                resolution = 'minute';
                filterAfter = null;
                setLiveMode(false);
                break;
            }
            case 'all':
                from = '2000-01-01';
                to = today;
                resolution = 'daily';
                filterAfter = null;
                setLiveMode(false);
                break;
            default: {
                const days = parseInt(range);
                const d = new Date(now);
                d.setDate(d.getDate() - days);
                from = isoDate(d);
                to = today;
                resolution = days <= 2 ? 'minute' : 'daily';
                filterAfter = null;
                setLiveMode(false);
                break;
            }
        }

        document.getElementById('from-date').value = from;
        document.getElementById('to-date').value = to;
        document.getElementById('resolution').value = resolution;
        loadChart();
    }

    // ── Chart ─────────────────────────────────────────────────

    function createChart() {
        const ctx = document.getElementById('mainChart');
        if (!ctx) return null;

        return new Chart(ctx, {
            type: 'line',
            data: {
                datasets: [
                    { label: 'Consumption', data: [], borderColor: COLORS.consumption, borderWidth: 1.5, pointRadius: 0, fill: false, tension: 0.2 },
                    { label: 'Generation',  data: [], borderColor: COLORS.generation,  borderWidth: 1.5, pointRadius: 0, fill: false, tension: 0.2 },
                    { label: 'Grid Export', data: [], borderColor: COLORS.export,      borderWidth: 1.5, pointRadius: 0, fill: false, tension: 0.2 },
                    { label: 'Grid Import', data: [], borderColor: COLORS.import,      borderWidth: 1.5, pointRadius: 0, fill: false, tension: 0.2 },
                    { label: 'EV Charging', data: [], borderColor: COLORS.diverted,    borderWidth: 1.5, pointRadius: 0, fill: false, tension: 0.2 },
                ],
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { intersect: false, mode: 'index' },
                scales: {
                    x: {
                        type: 'time',
                        time: { unit: 'hour', displayFormats: { hour: 'HH:mm', day: 'dd/MM' } },
                        grid: { color: 'rgba(255,255,255,0.05)' },
                    },
                    y: {
                        title: { display: true, text: 'kWh' },
                        beginAtZero: true,
                        grid: { color: 'rgba(255,255,255,0.05)' },
                    },
                },
                plugins: {
                    legend: { position: 'top', labels: { usePointStyle: true, pointStyle: 'line', padding: 20 } },
                    tooltip: { backgroundColor: '#1e1e2e', titleColor: '#e5e7eb', bodyColor: '#e5e7eb', borderColor: '#2e2e3e', borderWidth: 1 },
                },
            },
        });
    }

    function loadChart() {
        if (!chart) return;
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        const resolution = document.getElementById('resolution').value;
        if (!from || !to) return;

        // Adjust time axis
        const units = { minute: 'hour', hourly: 'hour', daily: 'day' };
        chart.options.scales.x.time.unit = units[resolution];

        const url = `/api/v1/energy/${resolution}?serial=${serial}&from=${from}&to=${to}`;

        fetch(url).then(r => r.json()).then(data => {
            if (!data) data = [];
            const timeKey = resolution === 'daily' ? 'date' : 't';
            const excludeEV = document.getElementById('exclude-ev').checked;

            // Client-side time filter (e.g. "Last 12h" trims to the rolling window)
            if (filterAfter) {
                data = data.filter(d => d[timeKey] >= filterAfter);
            }

            const evTotal = (d) => (d.diverted || 0) + (d.boosted || 0);
            const evOffset = (d) => excludeEV ? evTotal(d) : 0;
            chart.data.datasets[0].data = data.map(d => ({ x: d[timeKey], y: (d.import || 0) + (d.generation || 0) - (d.export || 0) - evOffset(d) }));
            chart.data.datasets[1].data = data.map(d => ({ x: d[timeKey], y: d.generation || 0 }));
            chart.data.datasets[2].data = data.map(d => ({ x: d[timeKey], y: d.export || 0 }));
            chart.data.datasets[3].data = data.map(d => ({ x: d[timeKey], y: d.import || 0 }));
            chart.data.datasets[4].data = data.map(d => ({ x: d[timeKey], y: evTotal(d) }));
            // Hide EV line when excluded
            chart.data.datasets[4].hidden = excludeEV;
            chart.update();

            updateSummary(data, excludeEV);
            updateExportLinks();
            pushURL();
        });
    }

    // ── Summary ───────────────────────────────────────────────

    function updateSummary(data, excludeEV) {
        const el = document.getElementById('summary');
        if (!data || data.length === 0) {
            el.style.display = 'none';
            return;
        }

        const totals = data.reduce((acc, d) => {
            acc.import += d.import || 0;
            acc.export += d.export || 0;
            acc.generation += d.generation || 0;
            acc.diverted += d.diverted || 0;
            acc.boosted += d.boosted || 0;
            return acc;
        }, { import: 0, export: 0, generation: 0, diverted: 0, boosted: 0 });

        const evTotal = totals.diverted + totals.boosted;
        let consumption = totals.import + totals.generation - totals.export;
        if (excludeEV) consumption -= evTotal;

        const selfUse = totals.generation > 0
            ? ((totals.generation - totals.export) / totals.generation * 100).toFixed(1)
            : '0.0';

        const consumptionLabel = document.querySelector('#summary .consumption .label');
        if (consumptionLabel) consumptionLabel.textContent = excludeEV ? 'Household Only Consumption' : 'Consumption';

        document.getElementById('sum-consumption').textContent = consumption.toFixed(1);
        document.getElementById('sum-generation').textContent = totals.generation.toFixed(1);
        document.getElementById('sum-export').textContent = totals.export.toFixed(1);
        document.getElementById('sum-import').textContent = totals.import.toFixed(1);
        document.getElementById('sum-diverted').textContent = evTotal.toFixed(1);
        // Show solar/grid split beneath the EV total
        const evDetail = document.getElementById('ev-detail');
        if (evDetail) {
            if (evTotal > 0) {
                evDetail.textContent = `${totals.diverted.toFixed(1)} solar · ${totals.boosted.toFixed(1)} grid`;
                evDetail.style.display = '';
            } else {
                evDetail.style.display = 'none';
            }
        }
        document.getElementById('sum-selfuse').textContent = selfUse;
        el.style.display = '';
    }

    function updateExportLinks() {
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        const resolution = document.getElementById('resolution').value;
        const base = `/api/v1/export?serial=${serial}&from=${from}&to=${to}&resolution=${resolution}`;

        const csvLink = document.getElementById('export-csv');
        const jsonLink = document.getElementById('export-json');
        if (csvLink) csvLink.href = base + '&format=csv';
        if (jsonLink) jsonLink.href = base + '&format=json';
    }

    // ── URL State ─────────────────────────────────────────────

    function pushURL() {
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        const resolution = document.getElementById('resolution').value;
        const excludeEV = document.getElementById('exclude-ev').checked;

        const params = new URLSearchParams();
        if (from) params.set('from', from);
        if (to) params.set('to', to);
        if (resolution) params.set('res', resolution);
        if (excludeEV) params.set('exclude-ev', '1');

        // Find active quick-range button
        const activeBtn = document.querySelector('.quick-range button.active');
        if (activeBtn) params.set('range', activeBtn.dataset.range);

        const url = '/' + (params.toString() ? '?' + params.toString() : '');
        history.replaceState(null, '', url);
    }

    function restoreFromURL() {
        const params = new URLSearchParams(window.location.search);
        if (!params.has('from') && !params.has('range')) return false;

        // Restore exclude-ev toggle
        const excludeEV = params.get('exclude-ev') === '1';
        document.getElementById('exclude-ev').checked = excludeEV;

        // If a quick-range is set, use it (recalculates dates from now)
        const range = params.get('range');
        if (range) {
            clearActiveButton();
            const btn = document.querySelector(`.quick-range button[data-range="${range}"]`);
            if (btn) btn.classList.add('active');
            selectRange(range);
            return true;
        }

        // Otherwise restore explicit from/to/resolution
        const from = params.get('from');
        const to = params.get('to');
        const resolution = params.get('res') || 'daily';

        if (from) document.getElementById('from-date').value = from;
        if (to) document.getElementById('to-date').value = to;
        document.getElementById('resolution').value = resolution;

        clearActiveButton();
        filterAfter = null;
        setLiveMode(false);
        loadChart();
        return true;
    }

    function isoDate(d) {
        return d.toISOString().slice(0, 10);
    }

    return { init };
})();
