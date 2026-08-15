function copyToClipboard(url) {
  navigator.clipboard
    .writeText(url)
    .then(() => {
      showCopyFeedback();
    })
    .catch((err) => {
      console.error("Failed to copy: ", err);
      showCopyFeedback("Failed to copy");
    });
}

function showCopyFeedback(message = "Copied!") {
  let feedback = document.querySelector(".copy-feedback");
  if (!feedback) {
    feedback = document.createElement("div");
    feedback.className = "copy-feedback";
    document.querySelector(".copy-button").parentNode.appendChild(feedback);
  }
  feedback.textContent = message;
  feedback.classList.add("show");

  setTimeout(() => {
    feedback.classList.remove("show");
  }, 2000);
}

if (window.location.pathname === "/feeds") {
  const getLatestFeedItemsOnPage = () => {
    const feedItems = document.querySelectorAll(
      ".details-wrapper details summary",
    );
    const latestFeedItemOnPage = feedItems[0].innerText;

    return { feedItems, latestFeedItemOnPage };
  };

  function getLocalStorageKey() {
    const url = new URL(window.location);
    const params = new URLSearchParams(url.search);
    const localStorageKey = `${params.getAll("keywords").join("+")}/${params.get(
      "location",
    )}`;

    return localStorageKey;
  }
  const pageTitle = "rssjobs";
  const pageTitleNewJobs = (nrOfNewPosts) =>
    `${pageTitle} - feed updated with ${nrOfNewPosts} ${
      nrOfNewPosts === 1 ? "post" : "posts"
    } `;

  function highlightLatestAdditions() {
    const { feedItems, latestFeedItemOnPage } = getLatestFeedItemsOnPage();
    const latestFeedItemLocalStorage =
      localStorage.getItem(getLocalStorageKey());

    if (latestFeedItemLocalStorage !== latestFeedItemOnPage) {
      const indexOfLatestPost = Array.from(feedItems).findIndex(
        (item) => item.innerText === latestFeedItemLocalStorage,
      );
      document.title = pageTitleNewJobs(indexOfLatestPost);

      const nrOfNewItems =
        indexOfLatestPost !== -1 ? indexOfLatestPost : feedItems.length;

      for (let i = 0; i < nrOfNewItems; i++) {
        feedItems[i].classList.add("new-entry");
      }
    }
  }

  function autoRefresh() {
    location.reload();
  }

  const thirtyMinutes = 30 * 60 * 1000;
  setInterval(autoRefresh, thirtyMinutes);

  window.addEventListener("load", () => {
    highlightLatestAdditions();
    const { latestFeedItemOnPage } = getLatestFeedItemsOnPage();
    localStorage.setItem(getLocalStorageKey(), latestFeedItemOnPage);
  });
  window.addEventListener("focus", () => {
    document.title = pageTitle;
  });
}
