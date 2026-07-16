// State Management
let state = {
    apiKey: localStorage.getItem('agentic_quota_api_key') || '',
    quotas: {},
    history: {},
    activeTab: 'active', // 'active' or 'history'
    selectedDays: 1, // timeframe selection: 1 or 7 days
    toggledOffQuotas: new Set(),
    autoRefreshInterval: parseInt(localStorage.getItem('agentic_quota_refresh') || '30'),
    activeTimers: [],
    refreshTimerId: null,
    isFetching: false
};

// DOM Elements
const elements = {
    connIndicator: document.getElementById('conn-indicator'),
    connText: document.getElementById('conn-text'),
    settingsBtn: document.getElementById('settings-btn'),
    refreshBtn: document.getElementById('refresh-btn'),
    refreshIcon: document.getElementById('refresh-icon'),
    alertBanner: document.getElementById('alert-banner'),
    alertMsg: document.getElementById('alert-msg'),
    emptyState: document.getElementById('empty-state'),
    quotaGrid: document.getElementById('quota-grid'),
    settingsModal: document.getElementById('settings-modal'),
    modalOverlay: document.getElementById('modal-overlay'),
    closeModalBtn: document.getElementById('close-modal-btn'),
    settingsForm: document.getElementById('settings-form'),
    apiKeyInput: document.getElementById('api-key-input'),
    saveKeyCheckbox: document.getElementById('save-key-checkbox'),
    autoRefreshSelect: document.getElementById('auto-refresh-select'),
    
    // Tabs & Sections
    tabActive: document.getElementById('tab-active'),
    tabHistory: document.getElementById('tab-history'),
    viewActive: document.getElementById('view-active'),
    viewHistory: document.getElementById('view-history'),
    chartLegend: document.getElementById('chart-legend'),
    timeframeToggle: document.getElementById('timeframe-toggle'),
    chartLoading: document.getElementById('chart-loading'),
    chartEmpty: document.getElementById('chart-empty'),
    chartContainer: document.getElementById('chart-container')
};

// SVG Settings for Circle Gauge
const SVG_RADIUS = 50;
const SVG_CIRCUMFERENCE = 2 * Math.PI * SVG_RADIUS; // ~314.16

// SVG Chart Configuration
const CHART_WIDTH = 800;
const CHART_HEIGHT = 380;
const CHART_MARGIN = { top: 40, right: 40, bottom: 50, left: 60 };

// Neon Color Palette for multiple graph lines
const PALETTE = ['#00f2fe', '#ff007f', '#39ff14', '#f1c40f', '#9b59b6', '#e67e22'];

function getQuotaColor(name, index) {
    return PALETTE[index % PALETTE.length];
}

// Initialize Application
function init() {
    setupEventListeners();
    
    // Set form and select values from current state
    if (state.apiKey) {
        elements.apiKeyInput.value = state.apiKey;
    }
    elements.autoRefreshSelect.value = state.autoRefreshInterval;
    
    // Initial fetch or prompt for key
    if (!state.apiKey) {
        showModal();
        showBanner('API Key is required to view quota metrics.', 'error');
        elements.quotaGrid.innerHTML = `
            <div class="empty-state" style="margin: 0 auto; width: 100%;">
                <div class="empty-icon">🔑</div>
                <h2>Authentication Required</h2>
                <p>Please click the settings gear or the button below to configure your API key.</p>
                <button onclick="showModal()" class="submit-btn" style="margin-top: 1rem; width: auto; padding: 0.6rem 1.5rem;">Configure API Key</button>
            </div>
        `;
        updateConnectionStatus('offline', 'Authentication Required');
    } else {
        refreshDashboardData();
        setupAutoRefresh();
    }
    
    // Start the countdown ticks (runs independent of fetch)
    startCountdownTicks();
}

// Setup Event Listeners
function setupEventListeners() {
    elements.settingsBtn.addEventListener('click', showModal);
    elements.closeModalBtn.addEventListener('click', hideModal);
    elements.modalOverlay.addEventListener('click', hideModal);
    elements.refreshBtn.addEventListener('click', () => refreshDashboardData(true));
    
    // Tab switching
    elements.tabActive.addEventListener('click', () => switchTab('active'));
    elements.tabHistory.addEventListener('click', () => switchTab('history'));
    
    // Timeframe toggle buttons
    if (elements.timeframeToggle) {
        const tfButtons = elements.timeframeToggle.querySelectorAll('.time-toggle-btn');
        tfButtons.forEach(btn => {
            btn.addEventListener('click', () => {
                const days = parseInt(btn.getAttribute('data-days'));
                if (state.selectedDays === days) return;
                
                tfButtons.forEach(b => b.classList.remove('active'));
                btn.classList.add('active');
                
                state.selectedDays = days;
                fetchHistoryData();
            });
        });
    }
    
    elements.settingsForm.addEventListener('submit', (e) => {
        e.preventDefault();
        const enteredKey = elements.apiKeyInput.value.trim();
        const remember = elements.saveKeyCheckbox.checked;
        
        state.apiKey = enteredKey;
        if (remember) {
            localStorage.setItem('agentic_quota_api_key', enteredKey);
        } else {
            localStorage.removeItem('agentic_quota_api_key');
        }
        
        hideModal();
        refreshDashboardData();
        setupAutoRefresh();
    });
    
    elements.autoRefreshSelect.addEventListener('change', (e) => {
        const value = parseInt(e.target.value);
        state.autoRefreshInterval = value;
        localStorage.setItem('agentic_quota_refresh', value);
        setupAutoRefresh();
    });
}

// Tab switcher
function switchTab(tabId) {
    if (state.activeTab === tabId) return;
    
    state.activeTab = tabId;
    
    if (tabId === 'active') {
        elements.tabActive.classList.add('active');
        elements.tabHistory.classList.remove('active');
        elements.viewActive.classList.remove('hidden');
        elements.viewHistory.classList.add('hidden');
        if (!state.quotas || Object.keys(state.quotas).length === 0) {
            elements.emptyState.classList.remove('hidden');
        }
        fetchQuotaData();
    } else {
        elements.tabActive.classList.remove('active');
        elements.tabHistory.classList.add('active');
        elements.viewActive.classList.add('hidden');
        elements.viewHistory.classList.remove('hidden');
        elements.emptyState.classList.add('hidden');
        fetchHistoryData();
    }
}

// Modal Controls
function showModal() {
    elements.settingsModal.classList.remove('hidden');
    elements.apiKeyInput.focus();
}

// Close Modal
function hideModal() {
    elements.settingsModal.classList.add('hidden');
}

// Banner controls
function showBanner(message, type = 'error') {
    elements.alertMsg.textContent = message;
    elements.alertBanner.className = 'banner';
    if (type === 'success') {
        elements.alertBanner.classList.add('success');
    }
    elements.alertBanner.classList.remove('hidden');
}

function hideBanner() {
    elements.alertBanner.classList.add('hidden');
}

// Connection Status Helpers
function updateConnectionStatus(status, text) {
    elements.connIndicator.className = 'status-dot';
    elements.connText.textContent = text;
    
    if (status === 'connected') {
        elements.connIndicator.classList.add('status-connected');
    } else if (status === 'unauthorized') {
        elements.connIndicator.classList.add('status-unauthorized');
    } else {
        elements.connIndicator.classList.add('status-offline');
    }
}

// Manage Auto Refresh
function setupAutoRefresh() {
    if (state.refreshTimerId) {
        clearInterval(state.refreshTimerId);
        state.refreshTimerId = null;
    }
    
    if (state.autoRefreshInterval > 0 && state.apiKey) {
        state.refreshTimerId = setInterval(() => {
            refreshDashboardData();
        }, state.autoRefreshInterval * 1000);
    }
}

// Unified Refresh Data Action
function refreshDashboardData(isManual = false) {
    if (state.activeTab === 'active') {
        fetchQuotaData(isManual);
    } else {
        fetchHistoryData(isManual);
    }
}

// Fetch Active Quotas from Server
async function fetchQuotaData(isManual = false) {
    if (state.isFetching) return;
    state.isFetching = true;
    
    elements.refreshIcon.classList.add('spinning');
    if (isManual) {
        elements.refreshBtn.disabled = true;
    }
    
    try {
        const response = await fetch('/api/v1/quota', {
            method: 'GET',
            headers: {
                'X-API-Key': state.apiKey,
                'Accept': 'application/json'
            }
        });
        
        if (response.status === 200) {
            const data = await response.json();
            hideBanner();
            updateConnectionStatus('connected', 'Connected');
            processQuotaData(data.quota || {});
        } else if (response.status === 401) {
            updateConnectionStatus('unauthorized', 'Unauthorized');
            showBanner('Unauthorized API Key. Please verify your X-API-Key settings.');
            showModal();
        } else if (response.status === 404) {
            hideBanner();
            updateConnectionStatus('connected', 'Connected');
            processQuotaData({});
        } else {
            const errData = await response.json().catch(() => ({}));
            const errMsg = errData.error || `Server error (${response.status})`;
            updateConnectionStatus('offline', 'Error');
            showBanner(errMsg);
        }
    } catch (error) {
        console.error('Fetch error:', error);
        updateConnectionStatus('offline', 'Offline');
        showBanner('Cannot connect to quota service. Ensure the server is running.');
    } finally {
        state.isFetching = false;
        elements.refreshIcon.classList.remove('spinning');
        elements.refreshBtn.disabled = false;
    }
}

// Fetch Historical Data from Server
async function fetchHistoryData(isManual = false) {
    if (state.isFetching) return;
    state.isFetching = true;
    
    elements.refreshIcon.classList.add('spinning');
    elements.chartLoading.classList.remove('hidden');
    elements.chartEmpty.classList.add('hidden');
    elements.chartContainer.classList.add('hidden');
    
    if (isManual) {
        elements.refreshBtn.disabled = true;
    }
    
    try {
        const [response, resetResponse] = await Promise.all([
            fetch(`/api/v1/quota/history?days=${state.selectedDays}`, {
                method: 'GET',
                headers: {
                    'X-API-Key': state.apiKey,
                    'Accept': 'application/json'
                }
            }),
            fetch(`/api/v1/quota/history/reset?days=${state.selectedDays}`, {
                method: 'GET',
                headers: {
                    'X-API-Key': state.apiKey,
                    'Accept': 'application/json'
                }
            })
        ]);
        
        if (response.status === 401 || resetResponse.status === 401) {
            updateConnectionStatus('unauthorized', 'Unauthorized');
            showBanner('Unauthorized API Key. Please verify your X-API-Key settings.');
            showModal();
            return;
        }

        if (response.status !== 200) {
            const errData = await response.json().catch(() => ({}));
            const errMsg = errData.error || `Server error (${response.status})`;
            updateConnectionStatus('offline', 'Error');
            showBanner(errMsg);
            return;
        }

        if (resetResponse.status !== 200) {
            const errData = await resetResponse.json().catch(() => ({}));
            const errMsg = errData.error || `Server error (${resetResponse.status})`;
            updateConnectionStatus('offline', 'Error');
            showBanner(errMsg);
            return;
        }

        const data = await response.json();
        const resetData = await resetResponse.json();
        hideBanner();
        updateConnectionStatus('connected', 'Connected');
        
        const historyMap = data.history || {};
        const resetMap = resetData.history || {};
        const mergedHistory = {};
        
        const allKeys = new Set([...Object.keys(historyMap), ...Object.keys(resetMap)]);
        for (const name of allKeys) {
            const pts = historyMap[name] ? [...historyMap[name]] : [];
            const resets = resetMap[name] || [];
            
            for (const r of resets) {
                if (r.reset_time) {
                    pts.push({
                        timestamp: r.reset_time,
                        value: 1.0,
                        isReset: true
                    });
                }
            }
            
            pts.sort((a, b) => {
                const diff = new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime();
                if (diff !== 0) return diff;
                return a.value - b.value;
            });
            
            mergedHistory[name] = pts;
        }

        state.history = mergedHistory;
        renderLegend();
        drawHistoryChart();
    } catch (error) {
        console.error('History fetch error:', error);
        updateConnectionStatus('offline', 'Offline');
        showBanner('Cannot connect to history service.');
    } finally {
        state.isFetching = false;
        elements.refreshIcon.classList.remove('spinning');
        elements.refreshBtn.disabled = false;
        elements.chartLoading.classList.add('hidden');
    }
}

// Process and parse active metrics
function processQuotaData(quotaMap) {
    state.quotas = quotaMap;
    const keys = Object.keys(quotaMap);
    
    if (keys.length === 0) {
        elements.emptyState.classList.remove('hidden');
        elements.quotaGrid.classList.add('hidden');
        elements.quotaGrid.innerHTML = '';
        state.activeTimers = [];
        return;
    }
    
    elements.emptyState.classList.add('hidden');
    elements.quotaGrid.classList.remove('hidden');
    
    const now = Date.now();
    state.activeTimers = [];
    
    const sortedKeys = keys.sort();
    let gridHTML = '';
    
    sortedKeys.forEach(name => {
        const q = quotaMap[name];
        
        let targetTime = null;
        if (q.reset_time && !q.reset_time.startsWith('0001-01-01')) {
            const parsedTime = new Date(q.reset_time).getTime();
            if (!isNaN(parsedTime)) {
                targetTime = parsedTime;
            }
        }
        
        // Fallback to reset_in_seconds if reset_time is invalid/missing
        if (!targetTime && q.reset_in_seconds > 0) {
            targetTime = now + (q.reset_in_seconds * 1000);
        }
        
        state.activeTimers.push({
            name: name,
            targetTime: targetTime
        });
        
        const cardState = getQuotaState(q.remaining_fraction);
        const percentText = (q.remaining_fraction * 100).toFixed(1) + '%';
        const formattedReset = formatTimestamp(q.reset_time);
        const strokeDashoffset = SVG_CIRCUMFERENCE * (1 - q.remaining_fraction);
        
        gridHTML += `
            <div class="quota-card state-${cardState}" data-quota-name="${name}">
                <div class="card-header">
                    <span class="card-title">${name}</span>
                    <span class="card-badge">${cardState}</span>
                </div>
                
                <div class="card-gauge-wrapper">
                    <svg class="gauge-svg" width="130" height="130">
                        <circle class="gauge-track" cx="65" cy="65" r="${SVG_RADIUS}"></circle>
                        <circle class="gauge-value" cx="65" cy="65" r="${SVG_RADIUS}" 
                                stroke-dasharray="${SVG_CIRCUMFERENCE}" 
                                stroke-dashoffset="${strokeDashoffset}"></circle>
                    </svg>
                    <div class="gauge-percentage">
                        <span>${percentText}</span>
                        <span class="gauge-label">remaining</span>
                    </div>
                </div>
                
                <div class="card-details">
                    <div class="detail-row">
                        <span class="detail-label">Reset Time</span>
                        <span class="detail-value" title="${q.reset_time}">${formattedReset}</span>
                    </div>
                    <div class="detail-row">
                        <span class="detail-label">Time Remaining</span>
                        <span class="detail-value countdown" id="countdown-${sanitizeId(name)}">--:--:--</span>
                    </div>
                </div>
            </div>
        `;
    });
    
    elements.quotaGrid.innerHTML = gridHTML;
    tickCountdowns();
}

// Render dynamic interactive Legend
function renderLegend() {
    const keys = Object.keys(state.history).sort();
    if (keys.length === 0) {
        elements.chartLegend.innerHTML = '';
        return;
    }
    
    let html = '';
    keys.forEach((name, idx) => {
        const color = getQuotaColor(name, idx);
        const isDisabled = state.toggledOffQuotas.has(name) ? 'disabled' : '';
        html += `
            <div class="legend-badge ${isDisabled}" data-quota-name="${name}">
                <span class="legend-color-dot" style="background: ${color}"></span>
                <span>${name}</span>
            </div>
        `;
    });
    elements.chartLegend.innerHTML = html;
    
    // Add event listeners to badge items for toggling lines
    elements.chartLegend.querySelectorAll('.legend-badge').forEach(badge => {
        badge.addEventListener('click', () => {
            const name = badge.getAttribute('data-quota-name');
            if (state.toggledOffQuotas.has(name)) {
                state.toggledOffQuotas.delete(name);
            } else {
                // Ensure at least one line remains visible
                const enabledCount = keys.length - state.toggledOffQuotas.size;
                if (enabledCount > 1) {
                    state.toggledOffQuotas.add(name);
                }
            }
            renderLegend();
            drawHistoryChart();
        });
    });
}

// Render multi-series SVG graph
function drawHistoryChart() {
    const activeSeriesNames = Object.keys(state.history)
        .filter(name => !state.toggledOffQuotas.has(name))
        .sort();
        
    if (activeSeriesNames.length === 0) {
        elements.chartEmpty.classList.remove('hidden');
        elements.chartContainer.classList.add('hidden');
        return;
    }
    
    // Collect all data points from active series to establish boundaries
    let allPoints = [];
    activeSeriesNames.forEach(name => {
        const pts = state.history[name] || [];
        pts.forEach(pt => {
            allPoints.push({
                timestamp: new Date(pt.timestamp).getTime(),
                value: pt.value
            });
        });
    });
    
    if (allPoints.length === 0) {
        elements.chartEmpty.classList.remove('hidden');
        elements.chartContainer.classList.add('hidden');
        return;
    }
    
    elements.chartEmpty.classList.add('hidden');
    elements.chartContainer.classList.remove('hidden');
    elements.chartContainer.innerHTML = ''; // Clear previous SVG
    
    // Set stable boundaries based on the selected timeframe range (24h or 7d)
    const tMax = Date.now();
    const tMin = tMax - (state.selectedDays * 24 * 60 * 60 * 1000);
    
    const chartWidth = CHART_WIDTH - CHART_MARGIN.left - CHART_MARGIN.right;
    const chartHeight = CHART_HEIGHT - CHART_MARGIN.top - CHART_MARGIN.bottom;
    
    const svgNamespace = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(svgNamespace, "svg");
    svg.setAttribute("viewBox", `0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`);
    svg.setAttribute("class", "chart-svg");
    
    // Defs for glowing filter
    const defs = document.createElementNS(svgNamespace, "defs");
    const filter = document.createElementNS(svgNamespace, "filter");
    filter.setAttribute("id", "chart-glow");
    filter.setAttribute("x", "-10%");
    filter.setAttribute("y", "-10%");
    filter.setAttribute("width", "120%");
    filter.setAttribute("height", "120%");
    
    const blur = document.createElementNS(svgNamespace, "feGaussianBlur");
    blur.setAttribute("stdDeviation", "3");
    blur.setAttribute("result", "blur");
    
    const merge = document.createElementNS(svgNamespace, "feMerge");
    const mergeNode1 = document.createElementNS(svgNamespace, "feMergeNode");
    mergeNode1.setAttribute("in", "blur");
    const mergeNode2 = document.createElementNS(svgNamespace, "feMergeNode");
    mergeNode2.setAttribute("in", "SourceGraphic");
    
    merge.appendChild(mergeNode1);
    merge.appendChild(mergeNode2);
    filter.appendChild(blur);
    filter.appendChild(merge);
    defs.appendChild(filter);
    svg.appendChild(defs);
    
    // 1. Draw horizontal grid lines and Y-axis labels
    const yGridValues = [0, 0.25, 0.5, 0.75, 1];
    yGridValues.forEach(val => {
        const y = CHART_MARGIN.top + (1 - val) * chartHeight;
        
        // Grid line
        const gridLine = document.createElementNS(svgNamespace, "line");
        gridLine.setAttribute("x1", CHART_MARGIN.left);
        gridLine.setAttribute("y1", y);
        gridLine.setAttribute("x2", CHART_WIDTH - CHART_MARGIN.right);
        gridLine.setAttribute("y2", y);
        gridLine.setAttribute("class", "chart-grid-line");
        svg.appendChild(gridLine);
        
        // Label
        const text = document.createElementNS(svgNamespace, "text");
        text.setAttribute("x", CHART_MARGIN.left - 10);
        text.setAttribute("y", y + 4);
        text.setAttribute("class", "chart-axis-text y-axis");
        text.textContent = `${(val * 100)}%`;
        svg.appendChild(text);
    });
    
    // 2. Draw vertical time grid lines and X-axis labels
    const numTicks = 5;
    for (let i = 0; i < numTicks; i++) {
        const ratio = i / (numTicks - 1);
        const t = tMin + ratio * (tMax - tMin);
        const x = CHART_MARGIN.left + ratio * chartWidth;
        
        if (i > 0 && i < numTicks - 1) {
            const vLine = document.createElementNS(svgNamespace, "line");
            vLine.setAttribute("x1", x);
            vLine.setAttribute("y1", CHART_MARGIN.top);
            vLine.setAttribute("x2", x);
            vLine.setAttribute("y2", CHART_HEIGHT - CHART_MARGIN.bottom);
            vLine.setAttribute("class", "chart-grid-line");
            svg.appendChild(vLine);
        }
        
        const text = document.createElementNS(svgNamespace, "text");
        text.setAttribute("x", x);
        text.setAttribute("y", CHART_HEIGHT - CHART_MARGIN.bottom + 20);
        text.setAttribute("class", "chart-axis-text x-axis");
        text.textContent = formatTickTime(t);
        svg.appendChild(text);
    }
    
    // 3. Project and Draw each active series
    const seriesProjectedData = {};
    const allHistoryKeys = Object.keys(state.history).sort();
    
    activeSeriesNames.forEach(name => {
        const pts = state.history[name] || [];
        if (pts.length === 0) return;
        
        const data = pts.map(pt => ({
            timestamp: new Date(pt.timestamp).getTime(),
            value: pt.value,
            isReset: pt.isReset
        })).sort((a, b) => {
            const diff = a.timestamp - b.timestamp;
            if (diff !== 0) return diff;
            return a.value - b.value;
        });
        
        const projectedPoints = data
            .filter(pt => pt.timestamp <= tMax)
            .map(pt => {
                const x = CHART_MARGIN.left + ((pt.timestamp - tMin) / (tMax - tMin)) * chartWidth;
                const y = CHART_MARGIN.top + (1 - pt.value) * chartHeight;
                return { x, y, timestamp: pt.timestamp, value: pt.value, isReset: pt.isReset };
            });
        
        seriesProjectedData[name] = projectedPoints;
        
        const colorIdx = allHistoryKeys.indexOf(name);
        const color = getQuotaColor(name, colorIdx);
        
        // Draw line path
        let pathD = "";
        projectedPoints.forEach((pt, idx) => {
            if (idx === 0) {
                pathD += `M ${pt.x} ${pt.y}`;
            } else {
                const prevPt = projectedPoints[idx - 1];
                // Always use step-after transition to represent discrete status checks
                pathD += ` L ${pt.x} ${prevPt.y} L ${pt.x} ${pt.y}`;
            }
        });
        
        const linePath = document.createElementNS(svgNamespace, "path");
        linePath.setAttribute("d", pathD);
        linePath.setAttribute("class", "chart-line-path");
        linePath.setAttribute("stroke", color);
        linePath.setAttribute("filter", "url(#chart-glow)");
        svg.appendChild(linePath);
        
        // Draw point dots
        projectedPoints.forEach(pt => {
            const circle = document.createElementNS(svgNamespace, "circle");
            circle.setAttribute("cx", pt.x);
            circle.setAttribute("cy", pt.y);
            circle.setAttribute("stroke", color);
            if (pt.isReset) {
                circle.setAttribute("r", "5.0");
                circle.setAttribute("class", "chart-data-point chart-reset-point");
                circle.style.setProperty('fill', color, 'important');
                circle.style.setProperty('stroke', '#ffffff', 'important');
                circle.style.setProperty('stroke-width', '2', 'important');
            } else {
                circle.setAttribute("r", "2.2");
                circle.setAttribute("class", "chart-data-point");
            }
            svg.appendChild(circle);
        });
    });
    
    // 4. Create and mount trackers
    const trackerGroup = document.createElementNS(svgNamespace, "g");
    svg.appendChild(trackerGroup);
    
    const trackerLine = document.createElementNS(svgNamespace, "line");
    trackerLine.setAttribute("y1", CHART_MARGIN.top);
    trackerLine.setAttribute("y2", CHART_HEIGHT - CHART_MARGIN.bottom);
    trackerLine.setAttribute("stroke", "rgba(255, 255, 255, 0.18)");
    trackerLine.setAttribute("stroke-width", "1.5");
    trackerLine.setAttribute("stroke-dasharray", "3 3");
    trackerLine.style.display = "none";
    svg.appendChild(trackerLine);
    
    elements.chartContainer.appendChild(svg);
    
    // Tooltip overlay element
    let tooltip = document.getElementById('chart-tooltip');
    if (!tooltip) {
        tooltip = document.createElement('div');
        tooltip.id = 'chart-tooltip';
        tooltip.className = 'chart-tooltip';
        elements.chartContainer.appendChild(tooltip);
    }
    
    const hideTracker = () => {
        trackerLine.style.display = "none";
        trackerGroup.innerHTML = '';
        tooltip.style.opacity = "0";
        tooltip.style.transform = "translateY(5px)";
    };
    
    // Mouse hover tracking
    svg.addEventListener('mousemove', (e) => {
        const rect = svg.getBoundingClientRect();
        const mouseX = e.clientX - rect.left;
        const svgX = (mouseX / rect.width) * CHART_WIDTH;
        
        if (svgX < CHART_MARGIN.left || svgX > CHART_WIDTH - CHART_MARGIN.right) {
            hideTracker();
            return;
        }
        
        // Find nearest points in all active series
        const hoverPoints = [];
        activeSeriesNames.forEach(name => {
            const pts = seriesProjectedData[name] || [];
            if (pts.length === 0) return;
            
            let closest = pts[0];
            let minDist = Math.abs(pts[0].x - svgX);
            for (let i = 1; i < pts.length; i++) {
                const dist = Math.abs(pts[i].x - svgX);
                if (dist < minDist) {
                    minDist = dist;
                    closest = pts[i];
                }
            }
            
            if (minDist < 60) {
                hoverPoints.push({
                    name: name,
                    pt: closest,
                    dist: minDist
                });
            }
        });
        
        if (hoverPoints.length === 0) {
            hideTracker();
            return;
        }
        
        // Align crosshair tracker to closest matching point's time coordinate
        hoverPoints.sort((a, b) => a.dist - b.dist);
        const bestPtX = hoverPoints[0].pt.x;
        const bestPtTime = hoverPoints[0].pt.timestamp;
        
        trackerLine.setAttribute('x1', bestPtX);
        trackerLine.setAttribute('x2', bestPtX);
        trackerLine.style.display = "block";
        
        // Render crosshair dots and tooltips
        trackerGroup.innerHTML = '';
        let tooltipValuesHTML = '';
        
        // Sort items alphabetically
        const sortedHoverPoints = hoverPoints.sort((a, b) => a.name.localeCompare(b.name));
        
        sortedHoverPoints.forEach(item => {
            const colorIdx = allHistoryKeys.indexOf(item.name);
            const color = getQuotaColor(item.name, colorIdx);
            
            const dot = document.createElementNS(svgNamespace, "circle");
            dot.setAttribute("cx", bestPtX);
            dot.setAttribute("cy", item.pt.y);
            dot.setAttribute("r", "5.5");
            dot.setAttribute("fill", color);
            dot.setAttribute("stroke", "#141124");
            dot.setAttribute("stroke-width", "2");
            trackerGroup.appendChild(dot);
            
            const labelSuffix = item.pt.isReset ? " (Reset)" : "";
            tooltipValuesHTML += `
                <div class="chart-tooltip-value">
                    <span class="chart-tooltip-marker" style="background: ${color}"></span>
                    <span style="color: var(--text-secondary); margin-right: 0.5rem;">${item.name}:</span>
                    <strong>${(item.pt.value * 100).toFixed(1)}%${labelSuffix}</strong>
                </div>
            `;
        });
        
        const containerRect = elements.chartContainer.getBoundingClientRect();
        const pointClientX = rect.left + (bestPtX / CHART_WIDTH) * rect.width;
        const tooltipLeft = pointClientX - containerRect.left;
        
        const avgY = hoverPoints.reduce((acc, curr) => acc + curr.pt.y, 0) / hoverPoints.length;
        const pointClientY = rect.top + (avgY / CHART_HEIGHT) * rect.height;
        const tooltipTop = pointClientY - containerRect.top;
        
        tooltip.innerHTML = `
            <div class="chart-tooltip-time">${formatTooltipTime(bestPtTime)}</div>
            ${tooltipValuesHTML}
        `;
        
        const tooltipWidth = tooltip.offsetWidth || 150;
        const tooltipHeight = tooltip.offsetHeight || 80;
        
        let leftPosition = tooltipLeft + 15;
        // Flip to the left side if the tooltip would overflow the right edge of the container
        if (leftPosition + tooltipWidth > containerRect.width - 10) {
            leftPosition = tooltipLeft - tooltipWidth - 15;
            if (leftPosition < 10) {
                leftPosition = 10;
            }
        }
        
        let topPosition = tooltipTop - tooltipHeight - 10;
        // Flip to the bottom if the tooltip would overflow the top edge of the container
        if (topPosition < 10) {
            topPosition = tooltipTop + 15;
        }
        
        tooltip.style.left = `${leftPosition}px`;
        tooltip.style.top = `${topPosition}px`;
        tooltip.style.borderColor = "rgba(255, 255, 255, 0.15)";
        tooltip.style.opacity = "1";
        tooltip.style.transform = "translateY(0)";
    });
    
    svg.addEventListener('mouseleave', hideTracker);
}

// Helpers
function getQuotaState(fraction) {
    if (fraction <= 0.20) return 'danger';
    if (fraction <= 0.50) return 'warning';
    return 'safe';
}

function sanitizeId(str) {
    return str.replace(/[^a-zA-Z0-9-_]/g, '_');
}

function formatTimestamp(isoString) {
    if (!isoString) return 'Never';
    try {
        const date = new Date(isoString);
        if (isNaN(date.getTime())) return 'Never';
        return date.toLocaleString(undefined, {
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit'
        });
    } catch (e) {
        return 'Never';
    }
}

function formatTickTime(timestampMs) {
    const d = new Date(timestampMs);
    if (state.selectedDays === 7) {
        return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
    }
    const h = String(d.getHours()).padStart(2, '0');
    const m = String(d.getMinutes()).padStart(2, '0');
    return `${h}:${m}`;
}

function formatTooltipTime(timestampMs) {
    const d = new Date(timestampMs);
    return d.toLocaleDateString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
}

// Run Timers every second
function startCountdownTicks() {
    setInterval(() => {
        tickCountdowns();
    }, 1000);
}

// Countdown handler
function tickCountdowns() {
    if (state.activeTimers.length === 0) return;
    
    const now = Date.now();
    
    state.activeTimers.forEach(timer => {
        const elementId = `countdown-${sanitizeId(timer.name)}`;
        const el = document.getElementById(elementId);
        if (!el) return;
        
        if (!timer.targetTime) {
            el.textContent = 'Never';
            el.classList.remove('expired');
            return;
        }
        
        const diffMs = timer.targetTime - now;
        
        if (diffMs <= 0) {
            el.textContent = '00:00:00';
            el.classList.add('expired');
            return;
        }
        
        el.classList.remove('expired');
        
        const diffSecs = Math.floor(diffMs / 1000);
        const hours = Math.floor(diffSecs / 3600);
        const minutes = Math.floor((diffSecs % 3600) / 60);
        const seconds = diffSecs % 60;
        
        const pad = (num) => String(num).padStart(2, '0');
        
        if (hours > 24) {
            const days = Math.floor(hours / 24);
            el.textContent = `${days}d ${pad(hours % 24)}h ${pad(minutes)}m`;
        } else {
            el.textContent = `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
        }
    });
}

// Boot application
document.addEventListener('DOMContentLoaded', init);
