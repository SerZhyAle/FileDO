(function () {
  // A standalone locale page (de, fr) is written in one language: it carries
  // data-page-lang, keeps it, and never writes it to the shared sza-lang key.
  var own = document.documentElement.dataset.pageLang;
  function setLang(l) {
    if (own) l = own;
    else if (l !== "ru" && l !== "en" && l !== "ua") l = "en";
    document.documentElement.dataset.lang = l;
    document.documentElement.lang = l === "ua" ? "uk" : l;
    if (!own) localStorage.setItem("sza-lang", l);
    document.querySelectorAll("[data-set-lang]").forEach(function (b) {
      b.setAttribute("aria-pressed", String(!own && b.dataset.setLang === l));
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
        '<p class="footer-tools-title" id="footerToolsTitle"><span data-l="ru">Другие инструменты SZA</span><span data-l="en">More tools by SZA</span><span data-l="ua">Інші інструменти SZA</span><span data-l="de">Weitere Tools von SZA</span><span data-l="fr">Autres outils de SZA</span></p><nav class="tools-grid" aria-labelledby="footerToolsTitle"><a href="https://serzhyale.github.io/FastMediaSorter_mob_v2/"><b>Fast Media Sorter &amp; Organizer</b><span>Android media sorter</span></a><a href="https://serzhyale.github.io/FastMediaSorter_Lite/"><b>Fast Media Sorter for Windows</b><span>Windows media sorter</span></a><a href="https://serzhyale.github.io/CyrFlip/"><b>CyrFlip</b><span>Windows layout fixer</span></a><a href="https://serzhyale.github.io/doc-html-translate/"><b>doc-html-translate</b><span>Windows ebook converter</span></a><a href="https://serzhyale.github.io/StreamsPlayer/"><b>StreamsPlayer</b><span>Windows stream player</span></a><a href="https://serzhyale.github.io/OneClickRunner/"><b>OneClickRunner</b><span>Windows tray launcher</span></a><a href="https://serzhyale.github.io/universal-agent-kit/"><b>Universal Agent Kit</b><span>AI-dev methodology</span></a><a href="https://sza.od.ua"><b>SZA</b><span>Portfolio</span></a></nav>',
      );
    });
  document.querySelectorAll("[data-set-lang]").forEach(function (b) {
    b.addEventListener("click", function () {
      if (!own) return setLang(b.dataset.setLang);
      // Choosing RU, EN or UA on a locale page writes sza-lang, then navigates.
      try {
        localStorage.setItem("sza-lang", b.dataset.setLang);
      } catch (e) {}
      if (b.dataset.go) location.href = b.dataset.go;
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
    '<span data-l="ru">Копировать</span><span data-l="en">Copy</span><span data-l="ua">Копіювати</span><span data-l="de">Kopieren</span><span data-l="fr">Copier</span>';
  var copiedLabel =
    '<span data-l="ru">Скопировано</span><span data-l="en">Copied</span><span data-l="ua">Скопійовано</span><span data-l="de">Kopiert</span><span data-l="fr">Copié</span>';
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
