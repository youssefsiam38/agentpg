// AgentPG Admin UI - Minimal JavaScript for HTMX enhancements

document.addEventListener('DOMContentLoaded', function() {
    // Auto-scroll message container when new content is added
    function scrollToBottom(container) {
        if (container) {
            container.scrollTop = container.scrollHeight;
        }
    }

    // Scroll messages container on page load
    const messagesContainer = document.getElementById('messages-container');
    if (messagesContainer) {
        scrollToBottom(messagesContainer);
    }

    // Handle HTMX events
    document.body.addEventListener('htmx:afterSwap', function(evt) {
        // Scroll to bottom when new messages arrive
        if (evt.detail.target.id === 'pending-response' ||
            evt.detail.target.id === 'messages-container') {
            scrollToBottom(document.getElementById('messages-container'));
        }
    });

    // Clear input after sending message
    document.body.addEventListener('htmx:afterRequest', function(evt) {
        if (evt.detail.elt.id === 'chat-form' && evt.detail.successful) {
            const input = evt.detail.elt.querySelector('input[name="message"]');
            if (input) {
                input.value = '';
                input.focus();
            }
        }
    });

    // Show loading indicator during HTMX requests
    document.body.addEventListener('htmx:beforeRequest', function(evt) {
        const indicator = evt.detail.elt.querySelector('.htmx-indicator');
        if (indicator) {
            indicator.style.display = 'inline-block';
        }
    });

    document.body.addEventListener('htmx:afterRequest', function(evt) {
        const indicator = evt.detail.elt.querySelector('.htmx-indicator');
        if (indicator) {
            indicator.style.display = 'none';
        }
    });

    // Handle keyboard shortcuts
    document.addEventListener('keydown', function(evt) {
        // Ctrl/Cmd + Enter to submit chat form
        if ((evt.ctrlKey || evt.metaKey) && evt.key === 'Enter') {
            const chatForm = document.getElementById('chat-form');
            if (chatForm) {
                htmx.trigger(chatForm, 'submit');
            }
        }
    });

    // Toggle details/summary elements
    document.querySelectorAll('details').forEach(function(details) {
        details.addEventListener('toggle', function() {
            // Animation or other effects can be added here
        });
    });

    // Format timestamps on page load
    document.querySelectorAll('[data-timestamp]').forEach(function(el) {
        const timestamp = el.getAttribute('data-timestamp');
        if (timestamp) {
            const date = new Date(timestamp);
            el.textContent = formatTimeAgo(date);
        }
    });

    // Utility: Format time ago
    function formatTimeAgo(date) {
        const now = new Date();
        const diff = now - date;
        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);

        if (seconds < 60) return 'just now';
        if (minutes < 60) return minutes + ' minute' + (minutes === 1 ? '' : 's') + ' ago';
        if (hours < 24) return hours + ' hour' + (hours === 1 ? '' : 's') + ' ago';
        return days + ' day' + (days === 1 ? '' : 's') + ' ago';
    }

    // Copy to clipboard functionality
    document.querySelectorAll('[data-copy]').forEach(function(el) {
        el.addEventListener('click', function() {
            const text = el.getAttribute('data-copy');
            navigator.clipboard.writeText(text).then(function() {
                // Show copied feedback
                const originalText = el.textContent;
                el.textContent = 'Copied!';
                setTimeout(function() {
                    el.textContent = originalText;
                }, 1500);
            });
        });
    });

    // Auto-refresh stats on dashboard
    const dashboardStats = document.getElementById('stats-container');
    if (dashboardStats && dashboardStats.hasAttribute('hx-trigger')) {
        // HTMX will handle the auto-refresh
        console.log('Dashboard auto-refresh enabled');
    }

    // Handle SSE connection for real-time updates
    function connectSSE() {
        const eventsEndpoint = '/api/dashboard/events';
        const eventSource = new EventSource(eventsEndpoint);

        eventSource.addEventListener('stats', function(e) {
            try {
                const stats = JSON.parse(e.data);
                updateDashboardStats(stats);
            } catch (err) {
                console.error('Failed to parse SSE data:', err);
            }
        });

        eventSource.onerror = function(e) {
            console.warn('SSE connection error, will retry');
            eventSource.close();
            // Retry after 5 seconds
            setTimeout(connectSSE, 5000);
        };
    }

    function updateDashboardStats(stats) {
        // Update stats if elements exist
        const elements = {
            'total-sessions': stats.total_sessions,
            'active-sessions': stats.active_sessions,
            'active-runs': stats.active_runs,
            'pending-runs': stats.pending_runs,
            'pending-tools': stats.pending_tools,
            'active-instances': stats.active_instances
        };

        for (const [id, value] of Object.entries(elements)) {
            const el = document.getElementById(id);
            if (el && value !== undefined) {
                el.textContent = value;
            }
        }
    }

    // Only connect SSE on dashboard page
    if (window.location.pathname.includes('/dashboard')) {
        // SSE is handled by HTMX polling instead for simplicity
        // Uncomment below to use SSE instead:
        // connectSSE();
    }
});

// Expose utility functions globally for inline scripts
window.AgentPG = {
    scrollToBottom: function(containerId) {
        const container = document.getElementById(containerId);
        if (container) {
            container.scrollTop = container.scrollHeight;
        }
    }
};
