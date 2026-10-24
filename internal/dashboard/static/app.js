// Fluctlight Hub app shell: screen routing between Sky, Read and Sys.
// Screens are overlays on one persistent scene (added in Task 2).

const SCREENS = ['sky', 'read', 'sys'];

const stage = document.getElementById('stage');

function screenFromHash() {
  const s = location.hash.slice(1);
  return SCREENS.includes(s) ? s : 'sky';
}

function go(screen) {
  stage.dataset.s = screen;
  document.querySelectorAll('.rail .ri').forEach((el) => {
    const on = el.dataset.r === screen;
    el.classList.toggle('on', on);
    if (on) el.setAttribute('aria-current', 'page');
    else el.removeAttribute('aria-current');
  });
}

addEventListener('hashchange', () => go(screenFromHash()));
go(screenFromHash());
