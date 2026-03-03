(function () {
  function toLocalDatetimeValue(date) {
    const pad = (value) => String(value).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
  }

  function initPostModal() {
    const modal = document.querySelector('[data-post-modal]');
    if (!modal) return;

    const openers = document.querySelectorAll('[data-open-post-modal]');
    const closers = modal.querySelectorAll('[data-close-post-modal]');
    const textArea = modal.querySelector('textarea[name="text"]');
    const returnInput = modal.querySelector('input[name="return_to"]');
    const scheduleInput = modal.querySelector('input[name="scheduled_at"]');

    const closeModal = () => {
      modal.hidden = true;
      modal.setAttribute('aria-hidden', 'true');
      document.body.classList.remove('modal-open');
    };

    const openModal = () => {
      modal.hidden = false;
      modal.setAttribute('aria-hidden', 'false');
      document.body.classList.add('modal-open');

      if (returnInput) {
        returnInput.value = `${window.location.pathname}${window.location.search}`;
      }
      if (scheduleInput && !scheduleInput.value) {
        const nextQuarter = new Date();
        nextQuarter.setMinutes(Math.ceil(nextQuarter.getMinutes() / 15) * 15);
        nextQuarter.setSeconds(0);
        nextQuarter.setMilliseconds(0);
        scheduleInput.value = toLocalDatetimeValue(nextQuarter);
      }

      window.setTimeout(() => {
        if (textArea) {
          textArea.focus();
        }
      }, 15);
    };

    openers.forEach((link) => {
      link.addEventListener('click', (event) => {
        event.preventDefault();
        openModal();
      });
    });

    closers.forEach((button) => {
      button.addEventListener('click', closeModal);
    });

    modal.addEventListener('click', (event) => {
      if (event.target === modal) {
        closeModal();
      }
    });

    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && !modal.hidden) {
        closeModal();
      }
    });
  }

  function initMediaInputs() {
    const groups = document.querySelectorAll("[data-media-group]");
    if (!groups.length) return;

    const mediaConfig = {
      auto: {
        placeholder: "https://example.com/media-or-article",
        hint: "Auto detect will infer media style from URL extension and domain."
      },
      image: {
        placeholder: "https://example.com/image.jpg",
        hint: "Image URLs typically end in .png, .jpg, .jpeg, .gif, or .webp."
      },
      video: {
        placeholder: "https://example.com/video.mp4",
        hint: "Video URLs typically end in .mp4, .mov, or .webm."
      },
      link: {
        placeholder: "https://example.com/article",
        hint: "Use a full article/product URL for link posts."
      }
    };

    groups.forEach((group) => {
      const typeSelect = group.querySelector("[data-media-type]");
      const mediaURL = group.querySelector("[data-media-url]");
      const mediaFile = group.querySelector("[data-media-file]");
      const uploadButton = group.querySelector("[data-media-upload-button]");
      const uploadStatus = group.querySelector("[data-media-upload-status]");
      const hint = group.querySelector("[data-media-hint]");
      if (!typeSelect || !mediaURL) return;

      const setUploadStatus = (text, isError) => {
        if (!uploadStatus) return;
        uploadStatus.textContent = text || "";
        if (isError) {
          uploadStatus.classList.add("text-danger");
        } else {
          uploadStatus.classList.remove("text-danger");
        }
      };

      const applyMediaHint = () => {
        const value = (typeSelect.value || "auto").toLowerCase();
        const selected = mediaConfig[value] || mediaConfig.auto;
        mediaURL.placeholder = selected.placeholder;
        if (hint) {
          hint.textContent = selected.hint;
        }
      };

      const uploadFile = async () => {
        if (!mediaFile || !mediaFile.files || !mediaFile.files.length) {
          setUploadStatus("Choose a file first.", true);
          return;
        }

        const file = mediaFile.files[0];
        const formData = new FormData();
        formData.append("file", file);

        setUploadStatus("Uploading…", false);
        uploadButton?.setAttribute("disabled", "disabled");

        try {
          const response = await fetch("/media/upload", {
            method: "POST",
            body: formData,
            credentials: "same-origin"
          });

          const payload = await response.json().catch(() => ({}));
          if (!response.ok) {
            throw new Error(payload.error || `Upload failed (${response.status})`);
          }

          if (payload.media_url) {
            mediaURL.value = payload.media_url;
          }
          if (payload.media_type && ["image", "video", "link"].includes(payload.media_type)) {
            typeSelect.value = payload.media_type;
          }
          applyMediaHint();
          setUploadStatus("Upload complete.", false);
        } catch (error) {
          setUploadStatus(error && error.message ? error.message : "Upload failed", true);
        } finally {
          uploadButton?.removeAttribute("disabled");
        }
      };

      uploadButton?.addEventListener("click", uploadFile);
      typeSelect.addEventListener("change", applyMediaHint);
      applyMediaHint();
    });
  }

  function setText(id, value) {
    const node = document.getElementById(id);
    if (!node) return;
    node.textContent = value;
  }

  function setError(message) {
    const errorNode = document.getElementById('analytics-error');
    if (!errorNode) return;
    if (!message) {
      errorNode.classList.add('analytics-hidden');
      errorNode.textContent = '';
      return;
    }
    errorNode.classList.remove('analytics-hidden');
    errorNode.textContent = message;
  }

  function renderChannelRows(rows) {
    const body = document.getElementById('analytics-channel-rows');
    if (!body) return;
    body.innerHTML = '';

    if (!Array.isArray(rows) || !rows.length) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 6;
      cell.className = 'muted';
      cell.textContent = 'No channel data for this week.';
      row.appendChild(cell);
      body.appendChild(row);
      return;
    }

    rows.forEach((item) => {
      const row = document.createElement('tr');
      const values = [
        item.display_name || '-',
        `${item.platform_name || '-'} (${item.platform_badge || '--'})`,
        item.status || '-',
        String(item.planned_posts || 0),
        String(item.sent_attempts || 0),
        String(item.failed_attempts || 0)
      ];

      values.forEach((value) => {
        const cell = document.createElement('td');
        cell.textContent = value;
        row.appendChild(cell);
      });

      body.appendChild(row);
    });
  }

  function renderPostRows(posts) {
    const body = document.getElementById('analytics-post-rows');
    if (!body) return;
    body.innerHTML = '';

    if (!Array.isArray(posts) || !posts.length) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 5;
      cell.className = 'muted';
      cell.textContent = 'No scheduled posts in this week window.';
      row.appendChild(cell);
      body.appendChild(row);
      return;
    }

    posts.forEach((post) => {
      const row = document.createElement('tr');
      const channels = Array.isArray(post.channels) ? post.channels.join(', ') : '-';
      const values = [
        `#${post.id || '-'}`,
        post.scheduled_at || '-',
        post.status || '-',
        channels,
        post.preview || '-'
      ];

      values.forEach((value) => {
        const cell = document.createElement('td');
        cell.textContent = value;
        row.appendChild(cell);
      });

      body.appendChild(row);
    });
  }

  function renderTopPost(snapshot) {
    const topPostNode = document.getElementById('analytics-top-post');
    if (!topPostNode) return;

    const topPost = snapshot && snapshot.top_post;
    if (!topPost || !topPost.post_id) {
      topPostNode.textContent = 'No successful attempts in this period yet.';
      topPostNode.classList.add('muted');
      return;
    }

    const preview = topPost.text_preview || '(no text preview)';
    const status = topPost.status || 'unknown';
    const attempts = topPost.successful_attempts || 0;
    topPostNode.textContent = `Post #${topPost.post_id} · ${attempts} successful attempt(s) · status: ${status} · ${preview}`;
    topPostNode.classList.remove('muted');
  }

  function initAnalytics() {
    const root = document.querySelector('[data-analytics-root]');
    if (!root) return;

    const weekInput = document.getElementById('analytics-week-start');
    const refreshButton = root.querySelector('[data-analytics-refresh]');

    const loadAnalytics = async () => {
      try {
        setError('');
        const week = weekInput ? weekInput.value : '';
        const query = week ? `?week=${encodeURIComponent(week)}` : '';
        const response = await fetch(`/analytics/data${query}`, {
          headers: {
            'Accept': 'application/json'
          }
        });

        if (!response.ok) {
          throw new Error(`Request failed (${response.status})`);
        }

        const payload = await response.json();
        const snapshot = payload.snapshot || {};

        setText('analytics-planned', String(snapshot.planned_posts || 0));
        setText('analytics-published', String(snapshot.published_attempts || 0));
        setText('analytics-failed', String(snapshot.failed_attempts || 0));
        setText('analytics-retries', String(snapshot.retry_attempts || 0));
        setText('analytics-range', `Window: ${snapshot.start || '-'} → ${snapshot.end || '-'}`);
        setText('analytics-generated', `Generated at: ${payload.generated_at || '-'}`);

        renderTopPost(snapshot);
        renderChannelRows(payload.channel_rows || []);
        renderPostRows(payload.upcoming_posts || []);
      } catch (error) {
        setError(error && error.message ? error.message : 'Failed to load analytics');
      }
    };

    if (refreshButton) {
      refreshButton.addEventListener('click', loadAnalytics);
    }
    if (weekInput) {
      weekInput.addEventListener('change', loadAnalytics);
    }

    loadAnalytics();
  }

  initPostModal();
  initMediaInputs();
  initAnalytics();
})();
