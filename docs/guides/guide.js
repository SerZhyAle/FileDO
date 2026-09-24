(function () {
  function setLang(l) {
    if (l !== "ru" && l !== "en" && l !== "ua") l = "en";
    document.documentElement.dataset.lang = l;
    document.documentElement.lang = l === "ua" ? "uk" : l;
    localStorage.setItem("sza-lang", l);
    document.querySelectorAll("[data-set-lang]").forEach(function (b) {
      b.setAttribute("aria-pressed", String(b.dataset.setLang === l));
    });
    var data = window.guideMeta && window.guideMeta[l];
    if (data) {
      document.title = data.title;
      var d = document.querySelector('meta[name="description"]');
      if (d) d.content = data.description;
    }
  }
  document
    .querySelectorAll(".site-footer .container")
    .forEach(function (container) {
      container.insertAdjacentHTML(
        "afterbegin",
        '<p class="footer-tools-title"><span data-l="ru">Другие инструменты SZA</span><span data-l="en">More tools by SZA</span><span data-l="ua">Інші інструменти SZA</span></p><div class="tools-grid"><a href="https://serzhyale.github.io/FastMediaSorter_mob_v2/"><b>FastMediaSorter v2</b><span>Android media sorter</span></a><a href="https://serzhyale.github.io/FastMediaSorter_Lite/"><b>Fast Media Sorter</b><span>Fast Media Sorter for Windows</span></a><a href="https://serzhyale.github.io/CyrFlip/"><b>CyrFlip</b><span>Windows layout fixer</span></a><a href="https://serzhyale.github.io/doc-html-translate/"><b>doc-html-translate</b><span>Windows ebook converter</span></a><a href="https://serzhyale.github.io/StreamsPlayer/"><b>StreamsPlayer</b><span>Windows stream player</span></a><a href="https://serzhyale.github.io/OneClickRunner/"><b>OneClickRunner</b><span>Windows tray launcher</span></a><a href="https://serzhyale.github.io/universal-agent-kit/"><b>Universal Agent Kit</b><span>AI-dev methodology</span></a><a href="https://sza.od.ua"><b>SZA</b><span>Portfolio</span></a></div>',
      );
    });
  document.querySelectorAll("[data-set-lang]").forEach(function (b) {
    b.addEventListener("click", function () {
      setLang(b.dataset.setLang);
    });
  });
  setLang(document.documentElement.dataset.lang || "en");
  var theme = document.getElementById("themeBtn");
  theme.addEventListener("click", function () {
    var next =
      document.documentElement.dataset.theme === "light" ? "dark" : "light";
    document.documentElement.dataset.theme = next;
    localStorage.setItem("sza-theme", next);
    document.querySelector('meta[name="theme-color"]').content =
      next === "light" ? "#eef3ea" : "#0a0f0a";
    theme.setAttribute(
      "aria-label",
      next === "light" ? "Switch to dark theme" : "Switch to light theme",
    );
  });
  // The feedback is a word in the page language, not a plain check mark: that
  // shape is action.confirm in ICON-SET, distinct from status.ok.
  var copyLabel =
    '<span data-l="ru">Копировать</span><span data-l="en">Copy</span><span data-l="ua">Копіювати</span>';
  var copiedLabel =
    '<span data-l="ru">Скопировано</span><span data-l="en">Copied</span><span data-l="ua">Скопійовано</span>';
  function copied(button) {
    button.innerHTML = copiedLabel;
    button.classList.add("done");
    setTimeout(function () {
      button.innerHTML = copyLabel;
      button.classList.remove("done");
    }, 1600);
  }
  function fallback(text, button) {
    var box = document.createElement("textarea");
    box.value = text;
    box.style.cssText = "position:fixed;opacity:0";
    document.body.appendChild(box);
    box.select();
    try {
      document.execCommand("copy");
    } catch (e) {}
    box.remove();
    copied(button);
  }
  document.querySelectorAll(".copy").forEach(function (button) {
    button.addEventListener("click", function () {
      var text = button.dataset.copy;
      if (navigator.clipboard) {
        navigator.clipboard.writeText(text).then(
          function () {
            copied(button);
          },
          function () {
            fallback(text, button);
          },
        );
      } else {
        fallback(text, button);
      }
    });
  });
  function openHash() {
    var item = document.getElementById(location.hash.slice(1));
    if (item && item.tagName === "DETAILS") {
      item.open = true;
      setTimeout(function () {
        item.scrollIntoView();
      }, 0);
    }
  }
  addEventListener("hashchange", openHash);
  openHash();
})();
