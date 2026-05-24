document.addEventListener('DOMContentLoaded', () => {
    let currentUser = null;

    // HTML entity escaper for XSS prevention
    function escapeHtml(str) {
        if (!str) return '';
        const div = document.createElement('div');
        div.appendChild(document.createTextNode(str));
        return div.innerHTML;
    }

    // Load active user details
    fetch('/api/me')
        .then(res => {
            if (!res.ok) throw new Error('Unauthenticated');
            return res.json();
        })
        .then(user => {
            currentUser = user;
            const profileHeader = document.getElementById('user-profile-header');
            if (profileHeader) {
                const safeName = escapeHtml(user.name);
                const safeAvatar = escapeHtml(user.avatar_url) || 'https://www.gravatar.com/avatar?d=mp';
                profileHeader.innerHTML = `
                    <span style="font-weight: 500; font-size: 0.9rem; color: var(--text-secondary);">${safeName}</span>
                    <img src="${safeAvatar}" class="user-avatar" alt="Avatar">
                `;
            }
        })
        .catch(() => {
            window.location.href = '/';
        });

    const sortSelect = document.getElementById('sort-select');
    const historyGrid = document.getElementById('history-grid');

    // Null guards — prevent TypeError if DOM elements are missing
    if (sortSelect) {
        sortSelect.addEventListener('change', () => {
            loadHistory(sortSelect.value);
        });
    }

    // Default load
    loadHistory('last_watched');

    function loadHistory(sortBy) {
        if (!historyGrid) return;
        
        historyGrid.innerHTML = `
            <div style="grid-column: 1/-1; padding: 5rem 0; text-align: center; color: var(--text-secondary);">
                Loading history cards...
            </div>
        `;

        fetch(`/api/history?sort_by=${sortBy}`)
            .then(res => {
                if (!res.ok) throw new Error('Failed to load history');
                return res.json();
            })
            .then(data => {
                renderHistory(data.items || []);
            })
            .catch(err => {
                if (!historyGrid) return;
                historyGrid.innerHTML = `
                    <div style="grid-column: 1/-1; padding: 5rem 0; text-align: center; color: #ef4444;">
                        Error loading watch history: ${escapeHtml(err.message)}
                    </div>
                `;
            });
    }

    function renderHistory(items) {
        if (!historyGrid) return;
        historyGrid.innerHTML = '';

        if (items.length === 0) {
            historyGrid.innerHTML = `
                <div class="empty-state">
                    <div class="empty-icon">🍿</div>
                    <h2>No watch history found</h2>
                    <p style="color: var(--text-muted); max-width: 400px; margin: 0 auto;">
                        Start streaming your favorite movies from Google Drive, Terabox, or direct links to build your history board.
                    </p>
                    <a href="/player" class="action-btn" style="text-decoration: none; margin-top: 1rem;">Go to Player</a>
                </div>
            `;
            return;
        }

        items.forEach(item => {
            const card = document.createElement('div');
            card.className = 'history-card glass-panel';

            const percentage = item.duration_seconds > 0 
                ? Math.min(Math.round((item.last_position / item.duration_seconds) * 100), 100) 
                : 0;

            const timeStr = formatDuration(item.total_watch_time);
            const dateStr = formatDateRelative(new Date(item.last_watched));
            const durationStr = formatSeconds(item.duration_seconds);

            // Sanitize all user-sourced strings for XSS safety
            const safeTitle = escapeHtml(item.title);
            const safeSourceType = escapeHtml(item.source_type);
            const safeThumbnail = escapeHtml(item.thumbnail_url) || 'https://images.unsplash.com/photo-1536440136628-849c177e76a1?w=500&auto=format&fit=crop&q=60';

            // Construct resume Link
            const resumeUrl = `/player?source=${encodeURIComponent(item.source_type)}&id=${encodeURIComponent(item.source_id)}&title=${encodeURIComponent(item.title)}`;

            card.innerHTML = `
                <div class="card-thumbnail-wrapper">
                    <img src="${safeThumbnail}" class="card-thumbnail" alt="Thumbnail">
                    <span class="source-badge ${safeSourceType}">${safeSourceType}</span>
                    <span class="duration-badge">${durationStr}</span>
                </div>
                <div class="card-body">
                    <h3 class="card-title" title="${safeTitle}">${safeTitle}</h3>
                    <div class="card-meta">
                        <span><strong>Watched:</strong> ${dateStr}</span>
                        <span><strong>Time Spent:</strong> ${timeStr} (${item.watch_count} session${item.watch_count > 1 ? 's' : ''})</span>
                        <span style="margin-top: 8px; font-weight: 600; color: var(--text-primary); display: flex; justify-content: space-between;">
                            <span>Progress</span>
                            <span>${percentage}%</span>
                        </span>
                        <div class="progress-bar-container">
                            <div class="progress-bar-fill" style="width: ${percentage}%;"></div>
                        </div>
                    </div>
                    <a href="${resumeUrl}" class="resume-btn">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <polygon points="5 3 19 12 5 21 5 3"></polygon>
                        </svg>
                        Resume Playback
                    </a>
                </div>
            `;

            historyGrid.appendChild(card);
        });
    }

    // Helper: format total cumulative seconds to readable hours/minutes
    function formatDuration(totalSeconds) {
        if (!totalSeconds || totalSeconds < 60) return `${totalSeconds || 0}s`;
        const mins = Math.floor(totalSeconds / 60);
        if (mins < 60) return `${mins}m`;
        const hrs = Math.floor(mins / 60);
        const remMins = mins % 60;
        return `${hrs}h ${remMins}m`;
    }

    // Helper: format standard video length
    function formatSeconds(secs) {
        if (!secs || secs <= 0) return '0:00';
        secs = Math.floor(secs); // Guard against floating point
        const h = Math.floor(secs / 3600);
        const m = Math.floor((secs % 3600) / 60);
        const s = secs % 60;
        return [
            h > 0 ? h : null,
            h > 0 && m < 10 ? '0' + m : m,
            s < 10 ? '0' + s : s
        ].filter(x => x !== null).join(':');
    }

    // Helper: relative dates (e.g. 2 hours ago)
    function formatDateRelative(date) {
        const now = new Date();
        const diffMs = now - date;
        const diffSecs = Math.floor(diffMs / 1000);
        
        if (diffSecs < 60) return 'just now';
        
        const diffMins = Math.floor(diffSecs / 60);
        if (diffMins < 60) return `${diffMins} minute${diffMins > 1 ? 's' : ''} ago`;
        
        const diffHrs = Math.floor(diffMins / 60);
        if (diffHrs < 24) return `${diffHrs} hour${diffHrs > 1 ? 's' : ''} ago`;
        
        const diffDays = Math.floor(diffHrs / 24);
        if (diffDays === 1) return 'yesterday';
        if (diffDays < 7) return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`;
        
        return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
    }
});
