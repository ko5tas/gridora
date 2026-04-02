// Gridora frontend — unified dashboard with SSE, Chart.js, custom plugins
const Gridora = (() => {
    const COLORS = {
        consumption: '#f97316',
        generation:  '#22c55e',
        export:      '#facc15',
        import:      '#ef4444',
        diverted:    '#3b82f6',
    };
    const ALPHA = '14'; // ~8% opacity hex suffix for fill

    // Dark theme defaults for Chart.js
    Chart.defaults.color = '#64748b';
    Chart.defaults.borderColor = '#334155';

    let chart = null;
    let serial = '';
    let milestones = [];
    let liveMode = true;
    let refreshTimer = null;
    let filterAfter = null;

    function init(s, ms) {
        serial = s;
        milestones = ms || [];
        chart = createChart();
        buildLegend();
        startSSE();
        bindControls();

        if (!restoreFromURL()) {
            selectRange('today');
        }

        window.addEventListener('popstate', () => restoreFromURL());
    }

    // ── Loading bar ──────────────────────────────

    function showLoading() {
        const el = document.getElementById('loading-bar');
        if (el) el.classList.add('active');
    }

    function hideLoading() {
        const el = document.getElementById('loading-bar');
        if (el) el.classList.remove('active');
    }

    // ── SSE (always connected, gauges shown/hidden) ──

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

        document.getElementById('grid-export-value').textContent = gridW < 0 ? Math.abs(gridW).toLocaleString() : '0';
        document.getElementById('grid-import-value').textContent = gridW > 0 ? gridW.toLocaleString() : '0';

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

        if (refreshTimer) clearInterval(refreshTimer);
        if (on) {
            refreshTimer = setInterval(loadChart, 5 * 60 * 1000);
        }
    }

    // ── Chart.js plugins ─────────────────────────

    const crosshairPlugin = {
        id: 'crosshair',
        afterDraw(chart) {
            if (!chart._active || !chart._active.length) return;
            const ctx = chart.ctx;
            const active = chart._active[0];
            const x = active.element.x;
            const area = chart.chartArea;

            ctx.save();
            ctx.setLineDash([4, 4]);
            ctx.lineWidth = 1;
            ctx.strokeStyle = '#64748b';
            ctx.beginPath();
            ctx.moveTo(x, area.top);
            ctx.lineTo(x, area.bottom);
            ctx.stroke();
            ctx.restore();
        }
    };

    const milestonePlugin = {
        id: 'milestones',
        afterDraw(chart) {
            if (!milestones || !milestones.length) return;
            const xScale = chart.scales.x;
            if (!xScale) return;
            const area = chart.chartArea;
            const ctx = chart.ctx;

            milestones.forEach(ms => {
                const ts = new Date(ms.date + 'T00:00:00').getTime();
                const x = xScale.getPixelForValue(ts);

                // Only draw if within visible area
                if (x < area.left || x > area.right) return;

                ctx.save();
                ctx.setLineDash([6, 3]);
                ctx.lineWidth = 2;
                ctx.strokeStyle = '#e2e8f0';
                ctx.beginPath();
                ctx.moveTo(x, area.top);
                ctx.lineTo(x, area.bottom);
                ctx.stroke();

                ctx.fillStyle = '#e2e8f0';
                ctx.font = '11px sans-serif';
                ctx.fillText(ms.label, x + 4, area.top + 14);
                ctx.restore();
            });
        }
    };

    // ── External tooltip ─────────────────────────

    function externalTooltip(context) {
        let el = document.getElementById('chart-tooltip');
        if (!el) {
            el = document.createElement('div');
            el.id = 'chart-tooltip';
            document.body.appendChild(el);
        }

        const tooltip = context.tooltip;
        if (tooltip.opacity === 0) {
            el.style.opacity = '0';
            return;
        }

        // Build tooltip using safe DOM methods
        el.textContent = ''; // clear previous content

        const titleDiv = document.createElement('div');
        titleDiv.style.cssText = 'color:#f8fafc;font-weight:600;margin-bottom:4px';
        titleDiv.textContent = tooltip.title[0];
        el.appendChild(titleDiv);

        tooltip.dataPoints.forEach(dp => {
            const row = document.createElement('div');
            row.style.cssText = 'display:flex;align-items:center;margin-top:2px';

            const color = dp.dataset.borderColor;
            const box = document.createElement('span');
            box.style.cssText = 'display:inline-flex;align-items:center;justify-content:center;width:14px;height:14px;border:2px solid ' + color + ';border-radius:3px;margin-right:6px;flex-shrink:0';

            const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
            svg.setAttribute('width', '8');
            svg.setAttribute('height', '8');
            svg.setAttribute('viewBox', '0 0 10 10');
            const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
            poly.setAttribute('points', '1,5 4,8 9,2');
            poly.setAttribute('fill', 'none');
            poly.setAttribute('stroke', color);
            poly.setAttribute('stroke-width', '2');
            svg.appendChild(poly);
            box.appendChild(svg);

            const label = document.createElement('span');
            label.textContent = dp.dataset.label + ': ';

            const val = document.createElement('strong');
            val.style.marginLeft = '4px';
            val.textContent = dp.formattedValue + ' kWh';

            row.appendChild(box);
            row.appendChild(label);
            row.appendChild(val);
            el.appendChild(row);
        });

        el.style.opacity = '1';
        const pos = context.chart.canvas.getBoundingClientRect();
        el.style.left = pos.left + window.scrollX + tooltip.caretX + 12 + 'px';
        el.style.top = pos.top + window.scrollY + tooltip.caretY - 20 + 'px';
    }

    // ── Chart creation ───────────────────────────

    function createChart() {
        const ctx = document.getElementById('mainChart');
        if (!ctx) return null;

        return new Chart(ctx, {
            type: 'line',
            plugins: [crosshairPlugin, milestonePlugin],
            data: {
                datasets: [
                    { label: 'Consumption', data: [], borderColor: COLORS.consumption, backgroundColor: COLORS.consumption + ALPHA, fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'Generation',  data: [], borderColor: COLORS.generation,  backgroundColor: COLORS.generation + ALPHA,  fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'Grid Export',  data: [], borderColor: COLORS.export,      backgroundColor: COLORS.export + ALPHA,      fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'Grid Import',  data: [], borderColor: COLORS.import,      backgroundColor: COLORS.import + ALPHA,      fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                    { label: 'EV Charging',  data: [], borderColor: COLORS.diverted,    backgroundColor: COLORS.diverted + ALPHA,    fill: true, tension: 0.3, pointRadius: 0, pointHitRadius: 8, borderWidth: 2 },
                ],
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { intersect: false, mode: 'index' },
                scales: {
                    x: {
                        type: 'time',
                        time: { unit: 'hour', displayFormats: { hour: 'HH:mm', day: 'dd/MM', week: 'dd/MM', month: 'MMM yyyy', quarter: 'QQQ yyyy', year: 'yyyy' } },
                        grid: { color: '#1e293b' },
                        ticks: { color: '#64748b', maxRotation: 45, font: { size: 10 } },
                    },
                    y: {
                        title: { display: true, text: 'kWh', color: '#64748b' },
                        beginAtZero: true,
                        grid: { color: '#334155' },
                        ticks: { color: '#64748b' },
                    },
                },
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        enabled: false,
                        external: externalTooltip,
                    },
                },
            },
        });
    }

    // ── Custom checkbox legend ────────────────────

    function buildLegend() {
        if (!chart) return;
        const legendEl = document.getElementById('chart-legend');
        if (!legendEl) return;
        legendEl.textContent = '';

        chart.data.datasets.forEach((ds, i) => {
            const item = document.createElement('div');
            item.className = 'legend-item';

            const box = document.createElement('span');
            box.className = 'legend-checkbox';
            box.style.borderColor = ds.borderColor;

            const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
            svg.setAttribute('width', '10');
            svg.setAttribute('height', '10');
            svg.setAttribute('viewBox', '0 0 10 10');
            const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
            poly.setAttribute('points', '1,5 4,8 9,2');
            poly.setAttribute('fill', 'none');
            poly.setAttribute('stroke', ds.borderColor);
            poly.setAttribute('stroke-width', '2');
            svg.appendChild(poly);
            box.appendChild(svg);

            const label = document.createElement('span');
            label.textContent = ds.label;

            item.appendChild(box);
            item.appendChild(label);

            item.addEventListener('click', () => {
                const visible = chart.isDatasetVisible(i);
                chart.setDatasetVisibility(i, !visible);
                item.classList.toggle('hidden', visible);
                chart.update();
            });

            legendEl.appendChild(item);
        });
    }

    // ── Controls ─────────────────────────────────

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

    // Auto-select resolution based on date span
    function autoResolution() {
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        if (!from || !to) return;

        const days = (new Date(to) - new Date(from)) / (1000 * 60 * 60 * 24);
        let resolution;
        if (days <= 2) resolution = 'minute';
        else if (days <= 14) resolution = 'hourly';
        else if (days <= 90) resolution = 'daily';
        else if (days <= 365) resolution = 'weekly';
        else if (days <= 730) resolution = 'monthly';
        else if (days <= 1825) resolution = 'quarterly';
        else resolution = 'yearly';

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
            case 'thisyear': {
                from = now.getFullYear() + '-01-01';
                to = today;
                resolution = 'daily';
                filterAfter = null;
                setLiveMode(false);
                break;
            }
            case 'all':
                from = '2000-01-01';
                to = today;
                resolution = 'monthly';
                filterAfter = null;
                setLiveMode(false);
                break;
            default: {
                const days = parseInt(range);
                const d = new Date(now);
                d.setDate(d.getDate() - days);
                from = isoDate(d);
                to = today;
                if (days <= 2) resolution = 'minute';
                else if (days <= 14) resolution = 'hourly';
                else resolution = 'daily';
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

    // ── Period string → Date converter ───────────

    function periodToDate(p) {
        if (!p) return null;

        // Weekly: "2025-W14"
        const wm = p.match(/^(\d{4})-W(\d{2})$/);
        if (wm) {
            const year = parseInt(wm[1]);
            const week = parseInt(wm[2]);
            const jan1 = new Date(year, 0, 1);
            const jan1Day = jan1.getDay(); // 0=Sun
            // %W: Monday-based, week 00 has days before first Monday
            const firstMonday = jan1Day <= 1 ? 1 - jan1Day : 8 - jan1Day;
            const dayOfYear = firstMonday + (week - 1) * 7;
            return new Date(year, 0, 1 + dayOfYear);
        }

        // Quarterly: "2025-Q2"
        const qm = p.match(/^(\d{4})-Q(\d)$/);
        if (qm) return new Date(parseInt(qm[1]), (parseInt(qm[2]) - 1) * 3, 1);

        // Yearly: "2025"
        if (/^\d{4}$/.test(p)) return new Date(parseInt(p), 0, 1);

        // Monthly: "2025-04"
        if (/^\d{4}-\d{2}$/.test(p)) return new Date(p + '-01T00:00:00');

        // Daily or datetime fallback
        return new Date(p);
    }

    // ── Chart data loading ───────────────────────

    function loadChart() {
        if (!chart) return;
        const from = document.getElementById('from-date').value;
        const to = document.getElementById('to-date').value;
        const resolution = document.getElementById('resolution').value;
        if (!from || !to) return;

        // Time axis unit mapping
        const units = {
            minute: 'hour', hourly: 'hour', daily: 'day',
            weekly: 'week', monthly: 'month', quarterly: 'quarter', yearly: 'year'
        };
        chart.options.scales.x.time.unit = units[resolution] || 'day';

        const url = `/api/v1/energy/${resolution}?serial=${serial}&from=${from}&to=${to}`;

        showLoading();
        fetch(url)
            .then(r => r.json())
            .then(data => {
                if (!data) data = [];

                // Determine the time key based on resolution
                const isPeriod = ['weekly', 'monthly', 'quarterly', 'yearly'].includes(resolution);
                const timeKey = isPeriod ? 'period' : (resolution === 'daily' ? 'date' : 't');
                const excludeEV = document.getElementById('exclude-ev').checked;

                // Client-side time filter (for "Last 12h")
                if (filterAfter && !isPeriod) {
                    data = data.filter(d => d[timeKey] >= filterAfter);
                }

                // Convert time values to x coordinates
                const toX = (d) => {
                    if (isPeriod) return periodToDate(d[timeKey]);
                    return d[timeKey];
                };

                const evTotal = (d) => (d.diverted || 0) + (d.boosted || 0);
                const evOffset = (d) => excludeEV ? evTotal(d) : 0;

                chart.data.datasets[0].data = data.map(d => ({ x: toX(d), y: (d.import || 0) + (d.generation || 0) - (d.export || 0) - evOffset(d) }));
                chart.data.datasets[1].data = data.map(d => ({ x: toX(d), y: d.generation || 0 }));
                chart.data.datasets[2].data = data.map(d => ({ x: toX(d), y: d.export || 0 }));
                chart.data.datasets[3].data = data.map(d => ({ x: toX(d), y: d.import || 0 }));
                chart.data.datasets[4].data = data.map(d => ({ x: toX(d), y: evTotal(d) }));
                chart.data.datasets[4].hidden = excludeEV;
                chart.update();

                updateSummary(data, excludeEV);
                updateExportLinks();
                pushURL();
            })
            .finally(() => hideLoading());
    }

    // ── Summary ──────────────────────────────────

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

        const evDetail = document.getElementById('ev-detail');
        if (evDetail) {
            if (evTotal > 0) {
                evDetail.textContent = totals.diverted.toFixed(1) + ' solar \u00b7 ' + totals.boosted.toFixed(1) + ' grid';
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
        const base = '/api/v1/export?serial=' + serial + '&from=' + from + '&to=' + to + '&resolution=' + resolution;

        const csvLink = document.getElementById('export-csv');
        const jsonLink = document.getElementById('export-json');
        if (csvLink) csvLink.href = base + '&format=csv';
        if (jsonLink) jsonLink.href = base + '&format=json';
    }

    // ── URL State ────────────────────────────────

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

        const activeBtn = document.querySelector('.quick-range button.active');
        if (activeBtn) params.set('range', activeBtn.dataset.range);

        const url = '/' + (params.toString() ? '?' + params.toString() : '');
        history.replaceState(null, '', url);
    }

    function restoreFromURL() {
        const params = new URLSearchParams(window.location.search);
        if (!params.has('from') && !params.has('range')) return false;

        const excludeEV = params.get('exclude-ev') === '1';
        document.getElementById('exclude-ev').checked = excludeEV;

        const range = params.get('range');
        if (range) {
            clearActiveButton();
            const btn = document.querySelector('.quick-range button[data-range="' + range + '"]');
            if (btn) btn.classList.add('active');
            selectRange(range);
            return true;
        }

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
