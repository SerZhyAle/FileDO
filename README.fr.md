# FileDO - Connaissez votre stockage avant de lui faire confiance

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/SerZhyAle/FileDO)](https://goreportcard.com/report/github.com/SerZhyAle/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2610070412-blue.svg)](https://github.com/SerZhyAle/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/SerZhyAle/FileDO)

**Détection de fausse capacité • Test de vitesse • Recherche de doublons • Comparaison de dossiers • Effacement de l'espace libre • Fichiers protégés par mot de passe • Disques virtuels**

</div>

---

## Démarrage Rapide

### Tâches Les Plus Courantes

```bash
# Vérifier la contrefaçon d'une clé USB/carte SD
filedo E: test del

# Test de performance du disque
filedo C: speed 100

# Effacement de l'espace libre
filedo D: fill 1000 del

# Recherche et gestion des doublons
filedo C: check-duplicates
filedo D:\Photos cd old del

# Copie avec suivi de progression
filedo folder C:\Source copy D:\Backup
filedo device E: copy F:\Archive
# Compter d'abord l'arborescence pour un temps restant exact (sinon la copie commence dès le premier fichier)
filedo folder C:\Source copy D:\Backup --precount

# Nettoyage rapide des dossiers
filedo folder C:\Temp wipe
filedo folder D:\Cache w

# Afficher les informations du disque
filedo C: info
```

### Vérification de lecture (check)

`filedo check <dossier>` lit les fichiers et marque comme endommagé celui qui se lit lentement (> 2,0 s) ou avec une erreur du périphérique. Les listes des fichiers endommagés et bien lus se trouvent dans `%LOCALAPPDATA%\FileDO\state` et désignent un fichier par son chemin, sa taille, son heure de modification ainsi que le volume et le fichier sur lesquels il a été enregistré : un fichier modifié, ou une copie au même chemin sur une autre carte sous la même lettre de lecteur, est relu ; les entrées des anciennes versions sans volume ne sont pas prises pour acquises. Un fichier uniquement en ligne (un espace réservé de OneDrive ou d'un autre cloud, ou un fichier hors ligne) n'est jamais ouvert - ni pendant un parcours, ni quand c'est justement lui qu'on vérifie -, donc rien n'est téléchargé. Il est compté comme `not-read(online-only)`, n'est jamais enregistré comme endommagé et rend l'exécution « vérification impossible » (code 2), sauf si un fichier endommagé a été trouvé (code 1).

### Installation

#### Option 1 - winget

```powershell
winget install SerZhyAle.FileDO
```

Installe les outils en ligne de commande (`filedo`, `filedo_check`, `filedo_fill`, `filedo_test`) et la fenêtre graphique `filedo_win` (une page par tâche), et les ajoute au `PATH`.

Également disponible dans le [Microsoft Store](https://apps.microsoft.com/detail/9PH1LPCMRG83) - les mêmes outils, installés et mis à jour par le Store.

#### Option 2 - Programme d'installation (setup EXE)

Téléchargez `FileDO-<version>-setup.exe` depuis les [releases](https://github.com/SerZhyAle/FileDO/releases/latest) et lancez-le. C'est cette option qui fait de FileDO un programme Windows ordinaire plutôt qu'un dossier d'exécutables:

- installe les fichiers dans `C:\Program Files\FileDO` et ajoute `filedo` au `PATH` système;
- crée une **entrée dans le menu Démarrer** et une **icône sur le bureau** pour la fenêtre FileDO (`filedo_win.exe`) et une seconde entrée du menu Démarrer, **FileDO Disk Manager**, qui ouvre directement le Gestionnaire de disques (`filedo_win.exe --disks`);
- enregistre l'**intégration à l'Explorateur**: un groupe `File DO..` dans le menu contextuel de tout fichier - Secure (garder, supprimer ou effacer l'original, ou un nom de conteneur aléatoire), Unsecure (en option, supprimer le conteneur ou démarrer aussitôt le fichier restauré), Wipe this file, Check this file, Info - ainsi que le type de document `.fd-sec` avec sa propre icône: le double-clic est exactement l'entrée Unsecure and start: une console y demande le mot de passe, restaure l'original sous son vrai nom dans `%LOCALAPPDATA%\FileDO\reveal` - un dossier que seuls ce compte et le système peuvent lire -, le confie au programme auquel appartient sa véritable extension, puis retire cette copie à la fermeture de la fenêtre de console. Sous Windows 11, le groupe se trouve sous *Afficher d'autres options*;
- enregistre le **type `.fdd` des conteneurs de disque** (fonctionnalité *Disk container files (.fdd)*): sa propre icône, un double-clic qui ouvre la fenêtre FileDO et monte le disque virtuel, et *Mount read-only* et *Unmount* dans le menu contextuel - voir la section *Disques virtuels* plus bas.

Le programme d'installation n'est pas signé numériquement: au premier lancement, Windows peut afficher *Windows a protégé votre ordinateur* puis demander les droits d'administrateur. Comparez le SHA256 (le fichier `.sha256` publié à côté du téléchargement; `certutil -hashfile FileDO-<version>-setup.exe SHA256`), puis choisissez *Informations complémentaires* et *Exécuter quand même*. Pourquoi l'avertissement apparaît, à quoi servent les droits d'administrateur et ce que FileDO ne fait jamais: [Windows warned you about FileDO](https://serzhyale.github.io/FileDO/guides/install-trust.html) (page en EN/RU/UA).

Ces trois derniers points sont des fonctionnalités que l'on peut décocher sur la page « Choose what to install » du programme d'installation, puis activer ou désactiver plus tard via **Modifier** dans « Applications et fonctionnalités ». Sans surveillance:

```powershell
FileDO-<version>-setup.exe /quiet
FileDO-<version>-setup.exe /uninstall
```

La même release publie aussi le `FileDO-<version>-windows-x64.msi` nu - c'est exactement ce fichier qui se trouve dans le setup EXE - pour les outils de déploiement qui veulent le paquet et les noms de fonctionnalités directement:

```powershell
msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main,ExplorerIntegration,DiskContainerIntegration,DesktopShortcut
msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main   # sans entrées de l'Explorateur, sans type .fdd, sans icône
```

La désinstallation retire tout ce que le programme d'installation a écrit, y compris les entrées de registre.

#### Option 3 - Microsoft Store (MSIX)

Un paquet, deux entrées: la tuile **FileDO** et la commande `filedo` dans le `PATH`. La version du Store n'a **pas** les entrées de l'Explorateur: un paquet ne peut les obtenir que via un gestionnaire shell signé, ce qui est un travail distinct. Elle prend **bien** en charge le type de fichier `.fd-sec`: un double-clic sur un conteneur ouvre la fenêtre FileDO sur la page *Ouvrir un fichier secret* avec ce conteneur déjà choisi, et le mot de passe y est demandé. Elle prend aussi en charge le type `.fdd`, mais la version du Store ne peut pas monter de disque virtuel: un double-clic ouvre la fenêtre FileDO sur ce conteneur, où l'on peut le lire, le vérifier et l'exporter - voir la section *Disques virtuels* plus bas.

#### Option 4 - Téléchargement manuel

1. **Télécharger**: obtenez `FileDO-<version>-windows-x64.zip` depuis les releases et décompressez-le où vous voulez
2. **GUI**: `filedo_win.exe` est dans l'archive - lancez-le à côté de `filedo.exe`
3. **Exécution**: depuis la ligne de commande ou via l'interface graphique

#### Intégration à l'Explorateur sans programme d'installation

winget, l'archive portable et `go install` ne lancent aucun programme d'installation et n'enregistrent donc rien. Le même groupe et le même type de document se demandent soi-même - et se reprennent de la même façon:

```powershell
filedo fdsec register              # pour cet utilisateur
filedo fdsec register -all-users   # pour toute la machine (console élevée requise)
filedo fdsec unregister            # retire exactement ce qui a été écrit
```

FileDO ne retire que ce qu'il a marqué comme sien: un type de document qui appartient désormais à un autre programme reste intact, et celui qui possédait `.fd-sec` avant FileDO le récupère.

---

## Opérations Principales

<table>
<tr>
<td width="50%">

### Test de Périphériques
```bash
# Informations
filedo C: info
filedo D: short

# Détection de fausse capacité
filedo E: test
filedo F: test del

# Test de performance
filedo C: speed 100
filedo D: speed max
```

</td>
<td width="50%">

### Opérations Fichiers et Dossiers
```bash
# Analyse de dossier
filedo C:\temp info
filedo . short

# Test de performance
filedo C:\data speed 100
filedo folder . speed max

# Opérations réseau
filedo \\server\share test
filedo network \\nas\backup speed 100

# Opérations par lots
filedo from commands.txt
filedo batch script.lst

# Nettoyage
filedo C:\temp clean
```

</td>
</tr>
</table>

---

## Fichiers secrets (`.fd-sec`)

Un fichier - ou un dossier, toute son arborescence - entre dans un conteneur, derrière un mot de passe, et
en ressort - depuis la ligne de commande, depuis le menu de l'Explorateur, ou depuis les pages du groupe
**Protéger** dans la fenêtre. Le vrai nom de l'original, sa taille réelle et ses horodatages y sont
scellés, et pour un dossier le chemin, la taille et les horodatages de chaque élément ; le conteneur
lui-même ne révèle que sa propre taille, son nom visible et ses horodatages (`rename` crée un bloc de
données anonyme). Rien sur le disque ne distingue un conteneur de dossier d'un conteneur de fichier.

```bash
# Empaqueter (le mot de passe est demandé deux fois, sans écho)
filedo report.docx secure

# Empaqueter et se débarrasser de l'original - récupérable, puis la manière forte
filedo report.docx secure del p:hunter2
filedo report.docx secure wipe p:hunter2

# Le récupérer sous son nom scellé, ou dans ce dossier
filedo report.fd-sec unsecure
filedo report.fd-sec unsecure here

# L'ouvrir dans le programme auquel il appartient, sans le déballer
filedo report.fd-sec reveal

# Un dossier s'empaquette de la même façon en un seul fichier, et revient avec toute son arborescence
filedo "Impots 2025" secure
filedo "Impots 2025.fd-sec" unsecure to D:\Restored

# La suite discrète : une clé plus dure, aucune trace d'alignement - mais seul FileDO l'ouvre
filedo report.docx secure suite2
```

Un dossier est empaqueté en entier - sous-dossiers vides compris - et restauré en entier : d'abord dans un
dossier temporaire neuf, mis à sa place seulement une fois chaque fichier vérifié, et jamais dans ni
par-dessus un dossier qui existe déjà (avec `-y`, sous un nom suffixé d'un numéro). Un dossier qui
contient une jonction, un lien symbolique ou un point de montage est refusé plutôt que suivi. `del` et
`wipe` ne retirent l'arborescence d'origine qu'après la relecture du conteneur, et seulement si le dossier
n'a pas changé depuis l'empaquetage. `reveal` ouvre un fichier : il refuse donc un conteneur de dossier et
renvoie vers `unsecure`.

`suite2` scelle un fichier (pas un dossier) avec la suite 2 au lieu de la suite 1 par défaut : une clé
Argon2id à 256 Mio repliée avec un poivre et XChaCha20-Poly1305 en trames de 64 Kio, si bien que le fichier
est du bruit dès son premier octet, sans même le motif de longueur par grappes de 512 octets de la suite 1.
Il garde le vrai nom, la taille réelle et l'heure du chiffrement, mais pas les horodatages propres de
l'original - le fichier restauré reçoit l'heure actuelle. Seul FileDO à partir de cette version l'ouvre :
les anciennes versions de FileDO le signalent comme endommagé ou comme un mauvais mot de passe, et les
applications FastMediaSorter ne peuvent pas l'ouvrir, donc la suite 1 reste celle par défaut. `unsecure`,
`reveal`, `fdsec info` et `fdsec verify` trouvent la suite eux-mêmes ; rien d'autre ne change, et un mot de
passe vide reste un simple camouflage.

Cinq choses dites franchement, car une fonction de sécurité qui se surestime vaut moins que rien :

- **Un mot de passe vide n'est qu'un camouflage - aucun secret.** Il est accepté, et chaque surface qui
  prend un mot de passe dit ce qu'il vaut au moment même où on le tape.
- **`wipe` réduit les chances de récupération et ne promet rien.** Sur SSD et sur les systèmes de fichiers
  à copie sur écriture ou journalisés, écraser un fichier sur place ne garantit pas que les anciens blocs
  ont disparu.
- **`wipe` n'écrase jamais un fichier qui a un autre lien physique.** L'écrasement sur place atteint tous
  les noms du fichier : l'original est donc conservé et l'exécution le dit ; retirez d'abord les liens en
  trop, ou utilisez `del`.
- **Une copie révélée vit dans `%LOCALAPPDATA%\FileDO\reveal`**, en lecture seule, lisible par ce compte
  et le système uniquement. Elle disparaît quand vous le dites, ou quand le programme qui l'a ouverte la
  relâche.
- **`unsecure start` - le double-clic - place sa copie dans ce même dossier protégé**, et non à côté du
  conteneur, puis la retire à la fermeture de la fenêtre de console. Cette copie est votre propre
  fichier, pas une vue en lecture seule; si le programme la garde ouverte à cet instant, le prochain
  démarrage de FileDO la balaie. Pour garder le fichier, utilisez `unsecure` tout court.
- **Après une coupure de courant, cette copie reste sur le disque jusqu'au prochain démarrage de FileDO**,
  qui la balaie. Rien d'autre ne le fera.

Il n'y a pas de clé de récupération : un mot de passe oublié, c'est un fichier perdu. L'original est
conservé tant que vous ne demandez pas son retrait, et rien n'est supprimé avant que le conteneur n'ait
été écrit, relu et vérifié. Les exécutables et les scripts ne sont jamais lancés depuis un conteneur -
ils sont extraits et leur emplacement est indiqué.

Dans la fenêtre, le groupe **Protéger** porte les trois mêmes opérations sous forme de pages : le mot de
passe est masqué, saisi deux fois à l'empaquetage, et transmis à `filedo.exe` hors de vue - il n'atteint
aucune ligne de commande, aucun rapport d'exécution et aucun fichier d'historique. Un double-clic sur un
`.fd-sec` n'ouvre pas cette fenêtre - il exécute **Unsecure and start** dans la console, exactement comme
l'entrée du même nom dans le menu de l'Explorateur.

---

## Disques virtuels (`.fdd`)

Un conteneur est un fichier `.fdd` ordinaire qui contient un volume entier. Monté, c'est une lettre de
lecteur comme une autre; démonté, c'est un fichier que l'on peut copier, sauvegarder ou lire avec FileDO
seul, sans le monter. Le format est publié en entier comme contrat `FDD-FORMAT`, et ce que doit faire un
programme qui le lit ou l'écrit comme `FDD-BEHAVIOUR` - les données ne dépendent donc pas de la survie de
FileDO.

```bash
# Créer un conteneur de 20 Go (plain: le fichier grandit à l'écriture), puis le monter
filedo vd new work.fdd 20G
filedo work.fdd mount

# Enregistrer, marquer comme fermé proprement, détacher
filedo X: unmount

# Un conteneur chiffré: vault demande toujours un mot de passe (deux fois)
filedo vd new private.fdd 5G vault

# Lire sans monter - sans droits d'administrateur, rien n'est écrit dans le conteneur
filedo work.fdd info
filedo work.fdd verify
filedo work.fdd export D:\work.vhd vhd

# Modifier tant qu'il n'est pas monté
filedo work.fdd grow 40G
filedo private.fdd pass
filedo private.fdd clone open-copy.fdd nopass
```

Quatre profils pour `vd new`: `plain` grandit à mesure qu'il se remplit, `fast` réserve toute la taille
tout de suite, `ram` garde le volume en mémoire tant qu'il est monté et l'enregistre dans le fichier toutes
les quelques secondes (un plantage perd ce qui a été écrit depuis le dernier enregistrement), et `vault` est
toujours chiffré. `seal` écrit une copie en lecture seule pour toujours, `clone` une copie modifiable avec
sa propre identité, `compact` rend au lecteur la place inutilisée du fichier. `format` efface le volume
(`fs ntfs` ou `fs exfat`) et `destroy` supprime le fichier du conteneur (`wipe` l'écrase d'abord): les deux
demandent d'abord, et les deux refusent un conteneur monté - aucune option ne lève ce contrôle. `chkdsk` vérifie le volume d'un conteneur non monté sans rien écrire, et `chkdsk fix` y lance `chkdsk /f` - la réparation d'un conteneur qui n'a pas été fermé proprement - en demandant d'abord.
`vd add work.fdd as work` lui donne un nom court, `vd list` et `vd status` montrent ce qui est connu et ce
qui est monté, et `vd auto work logon` monte un conteneur camouflé à l'ouverture de votre session.
`vd guard on` active le gardien d'arrêt: quand votre session se termine - un arrêt, un redémarrage ou une
déconnexion, jamais la mise en veille -, il enregistre d'abord les disques `ram`, puis démonte chaque conteneur
monté, pour que chacun soit fermé proprement; `vd guard status` montre sa dernière exécution. Un `.vhd`,
`.vhdx` ou `.iso` se monte par les moyens de Windows lui-même: `filedo disk.vhdx mount`.

Deux mots, deux promesses différentes, et FileDO ne les confond jamais:

- **Sans mot de passe, un conteneur est camouflé, pas chiffré.** Le camouflage soustrait le volume à un
  coup d'œil rapide et aux outils qui cherchent des images disque - mais pas à quiconque a le fichier et
  FileDO.
  `info` dit lequel des deux cas vous avez devant vous.
- **Avec un mot de passe, il est chiffré**: sans lui, le fichier est illisible. Il n'y a aucune
  récupération - un mot de passe oublié est un conteneur perdu. `pass` change le mot de passe sans réécrire
  les données; les copies du fichier faites auparavant s'ouvrent donc toujours avec l'ancien.
- **Un mot de passe n'est jamais retiré sur place.** Un nouveau mot de passe vide est refusé; à la place,
  `clone <new.fdd> nopass` écrit une copie camouflée, et l'original chiffré reste tel quel.
- **Le chiffrement protège le fichier, pas un volume monté.** Tant qu'il est monté, tout programme que vous
  lancez peut le lire, comme n'importe quel lecteur. Un `export` d'un conteneur chiffré n'est pas chiffré
  non plus, et avec `ram` et un mot de passe, Windows peut paginer des données non chiffrées dans le
  fichier d'échange.
- **`verify` lit, il ne prouve rien.** Il lit les en-têtes, la carte et chaque cluster alloué et signale les
  dommages (code 4), mais le format 1.0 ne garde aucune somme de contrôle des données: elles sont donc
  lues, pas vérifiées.

Ce qu'il faut à la plateforme, dit franchement:

- **Windows uniquement.** La lettre de lecteur vient de l'initiateur iSCSI intégré à Windows, qui parle à un
  serveur de blocs dans `filedo.exe`; ce serveur n'écoute que sur 127.0.0.1 - rien ne quitte cet
  ordinateur, et le conteneur ne passe jamais par aucun réseau - sauf si vous le partagez vous-même (voir plus bas).
- **Monter demande des droits d'administrateur.** `mount`, `unmount`, `save`, `format`, `chkdsk` et `vd auto` demandent à
  Windows l'accord d'un administrateur pour l'étape de l'initiateur; le mot de passe n'y va jamais, et le
  serveur de blocs lui-même ne tourne jamais avec des droits élevés. Un lot n'affiche jamais cette
  demande - lancez-le depuis une console administrateur. Si le service Initiateur iSCSI de Microsoft ne
  peut pas être utilisé, l'exécution se termine avec le code 7 et rien n'est monté.
- **La version du Microsoft Store ne peut pas monter de disque.** Une application empaquetée ne peut ni
  configurer l'initiateur iSCSI ni demander des droits d'administrateur, donc `mount`, `unmount`, `save`,
  `format`, `chkdsk`, `vd auto`, `vd guard` et `vd register` s'y terminent avec le code 6. `info`, `verify`, `export`,
  `compact`, `grow`, `seal`, `clone`, `pass`, `destroy`, `vd new`, `vd list`, `vd status`, `vd add` et
  `vd forget` y fonctionnent aussi; pour monter, prenez le setup ou la version portable sur GitHub.
- **Un montage survit à la fenêtre.** Fermer la fenêtre FileDO laisse le lecteur et son serveur en place,
  et une nouvelle fenêtre les liste de nouveau; démontez dans le Gestionnaire de disques, sur les pages du groupe *Disques* ou avec
  `filedo X: unmount`.

Dans le **Gestionnaire de disques**, sélectionnez un disque sur fichier enregistré et **Partager via Fast Media Sorter & Sharing..** dans les détails ou le menu Plus (`Ctrl+Shift+S`). Démarrez d’abord Share Manager. Si le worker est inaccessible, le dialogue propose une nouvelle vérification et la [page d’installation](https://serzhyale.github.io/FastMediaSorter_Lite/) ; cela ne prouve pas une installation absente. Activez si nécessaire le composant FileDO facultatif. L’installateur de bureau n’est pas signé. Lecture et écriture par défaut, la lecture seule se coche ; les disques sealed restent en lecture seule. Choisissez le nom du dossier et éventuellement son ouverture aux appareils associés. Un disque monté par FileDO est déconnecté proprement après confirmation ; la RAM est enregistrée par la déconnexion normale. Acceptez explicitement de désactiver le montage automatique FileDO ; un mode `keep` manuel s’arrête à la déconnexion. Le démarrage FMS s’active séparément. **Fermer le disque partagé** conserve l’enregistrement ; **Arrêter le partage** le supprime avec sa clé de démarrage enregistrée. En cas d’échec, les étapes terminées restent appliquées : actualisez, réessayez l’action restante ou arrêtez le partage avant de rétablir explicitement le montage local et la politique automatique antérieure. Associez les appareils via Share Manager. Les fichiers sont publiés via SFTP, sans partage SMB Windows. Le mode session exige une connexion ; un service fonctionne sans connexion. Disques sur partition et contrôles externes Store non vérifiés sont indisponibles ici ; utilisez la version de bureau. La page pas a pas - preconditions, chaque dialogue et le retablissement - est [Partager les disques FileDO avec Fast Media Sorter](https://serzhyale.github.io/FileDO/guides/fms-sharing.html).

**Partage d'un disque via Fast Media Sorter for Windows (facultatif).** Si Fast Media Sorter for Windows est
installé, `vd share work.fdd on` propose un disque aux téléphones et tablettes que vous lui avez associés,
comme un dossier de plus ; `vd open`, `vd close` et `vd autostart` l'ouvrent pour eux, le ferment et le
rouvrent après un redémarrage. FileDO n'ouvre aucune connexion réseau pour cela - il parle à ce programme par
un canal local, et c'est ce programme qui sert les fichiers. Un disque chiffré se déverrouille ici, sur ce PC,
jamais depuis le téléphone ; tant qu'il est ouvert, chaque appareil associé peut lire ses fichiers, et `vd
autostart` garde son mot de passe sur ce PC, protégé pour ce programme, jusqu'à ce que vous désactiviez le
démarrage automatique, arrêtiez le partage ou changiez le mot de passe. Un disque partagé est détenu par ce
programme : `mount` le refuse tant qu'il le détient ; `vd status` indique qui le détient. Seule l'édition
Serveur de Fast Media Sorter for Windows garde les disques partagés disponibles quand personne n'est connecté.

Codes de sortie d'une commande de conteneur: 0 terminé, 2 commande incorrecte, 3 mauvais mot de passe,
4 endommagé, 5 erreur d'E/S, 6 non pris en charge, 7 transport indisponible, 8 occupé (monté, ouvert ou
verrouillé). Un lot garde 0/1/2.

Dans l'Explorateur, la fonctionnalité *Disk container files (.fdd)* du setup (`DiskContainerIntegration`)
donne à `.fdd` son icône et trois entrées qui ouvrent la fenêtre FileDO. Un double-clic monte: un conteneur
camouflé fermé proprement tout de suite; un conteneur chiffré demande son mot de passe dans la fenêtre,
jamais sur une ligne de commande; un conteneur qui n'a pas été fermé proprement le dit d'abord, avec l'heure
de son dernier enregistrement complet, et attend; un conteneur déjà monté ouvre son lecteur. Le menu
contextuel ajoute *Mount read-only* et *Unmount* (sous Windows 11, sous *Afficher d'autres options*). Sans
programme d'installation, `filedo vd register` écrit la même chose (`-all-users` pour toute la machine), et
`filedo vd unregister` la retire. Dans la fenêtre, le groupe **Disques** a une page par opération et nomme
les deux protections exactement comme la console.

Le **Gestionnaire de disques** (en anglais *Disk manager*) est une seconde fenêtre du même `filedo_win.exe` - pas un nouveau programme - avec une ligne par disque virtuel: les disques de votre liste, ceux qui sont montés maintenant et les images VHD, VHDX ou ISO que FileDO a montées. Chaque ligne dit son état en toutes lettres à côté d'une icône (*Monté*, *Monté, lecture seule*, *Monté, 180 MiB non enregistrés* pour un disque `ram`, *Serveur disparu - volume hors ligne*, *Image montée*, *Fichier absent*, *Un autre conteneur se trouve à ce chemin*, *Illisible*, *Mal fermé*, *Non monté*), et la liste se tient à jour sans actualisation manuelle. Autour d'elle: une barre d'outils (*Nouveau disque..*, *Ajouter..*, *Monter*, *Démonter*, *Ouvrir*, *Enregistrer*, *Autres actions*, *Actualiser*, *Aide*, *Fenêtre principale*), une ligne de filtre et un volet de détails avec les boutons qui s'appliquent; un bouton inutilisable dit pourquoi dans son info-bulle et dans le volet, et la version du Microsoft Store, qui ne peut jamais monter, masque les commandes de montage au lieu de les griser. Un double-clic ou Entrée monte un disque au repos et ouvre le lecteur d'un disque monté - il ne démonte jamais; faites glisser des fichiers `.fdd` sur la liste pour les ajouter, ou un `.vhd`, `.vhdx` ou `.iso` pour monter une image après une confirmation. *Nouveau disque..* demande d'abord où le disque garde ses données: *Fichier sur un lecteur..* ouvre la page *Créer un disque* dans la fenêtre principale, *Partition dans l'espace libre..* la boîte de dialogue de partition du gestionnaire lui-même - une carte de l'espace libre de chaque disque GPT, la taille, le profil, l'étiquette et le nom (la version du Microsoft Store ne propose que le fichier). Pour un disque dans un fichier, *Exporter*, *Compacter*, *Agrandir*, *Copie scellée*, *Cloner*, *Changer le mot de passe*, *Formater* et *Détruire* ouvrent leur page de tâche dans la fenêtre principale avec le conteneur choisi; *Formater* et *Détruire* y gardent la confirmation à saisir. La ligne d'un [disque sur partition](#disques-sur-partition) dit *Partition non connectée* ou *Partition modifiée* quand il n'est plus là où il a été enregistré, propose *Image vers un fichier..* pour le copier dans un nouveau fichier `.fdd`, et il se supprime dans le gestionnaire lui-même: *Détruire* demande de saisir son nom et peut d'abord écraser la partition. *Adopter une partition..* (sous *Autres actions*, ou par un clic droit sur un espace vide) liste les partitions FileDO des disques de cet ordinateur qui ne sont pas dans la liste et ajoute celle qui est choisie sous un nom; la version du Store masque *Image vers un fichier* et *Adopter une partition*.

On l'ouvre par l'entrée **FileDO Disk Manager** du menu Démarrer (le programme d'installation l'ajoute; elle lance `filedo_win.exe --disks`), par le bouton **«Gestionnaire de disques»** en haut à droite de la fenêtre FileDO ou la première ligne de son groupe *Disques* (aussi **Ctrl+Shift+D**), ou avec `filedo_win.exe --disks`; son bouton **«Fenêtre principale»** ramène la fenêtre FileDO au premier plan. **F1** ouvre l'aide - ce que dit une ligne, le tableau des touches, des liens. À la première ouverture, une fenêtre de bienvenue explique ce qu'est un disque virtuel et dit: *Camouflé, pas chiffré: quiconque a ce fichier et FileDO peut le lire. Il s'ouvre sans mot de passe.* *Chiffré: il ne s'ouvre qu'avec son mot de passe.* Elle précise aussi que Windows demande l'accord d'un administrateur à chaque montage et démontage - et, pour un disque sur partition, aussi à chaque lecture; elle propose *Créer mon premier disque..*, *Ajouter un fichier .fdd que j'ai déjà..*, *Lire le guide sur le site* et *Pas maintenant*. Le tableau des touches et les images sont dans le [guide des disques virtuels](https://serzhyale.github.io/FileDO/guides/virtual-disks.html#manager). Fermer le gestionnaire ne démonte jamais un disque.

### Disques sur partition

Un disque virtuel peut aussi vivre dans une partition plutôt que dans un fichier. FileDO crée une nouvelle
partition GPT dans l'espace non alloué d'un disque et la remplit, entière, d'un seul conteneur; rien n'est
modifié hors de cet espace libre - FileDO ne convertit jamais une partition existante, ne la redimensionne
jamais et n'y écrit jamais. Disques GPT uniquement: les disques MBR, les disques dynamiques, les espaces de
stockage (Storage Spaces), les disques iSCSI, amovibles et USB sont refusés, et `vd disks` en donne la raison
pour chacun. Vous gagnez un disque qu'aucun système de fichiers hôte ne porte, auquel Windows ne donne pas de
lettre et qu'il ne propose pas de formater, avec son espace réservé; vous ne gagnez pas de vitesse.

```bash
# Tous les disques, leurs partitions et leur espace libre - chaque zone utilisable ou non, et pourquoi (sans droits d'administrateur)
filedo vd disks

# Un disque sur partition occupant toute une zone libre (la ligne create qu'affiche vd disks), nommé data
filedo vd new part disk:{GPT-GUID} size max at <offset> as data
filedo vd mount data

# Le copier dans un fichier .fdd ordinaire, lisible sans droits d'administrateur
filedo vd image data to D:\data.fdd

# Enregistrer une partition FileDO que ce PC ne connaît pas (un disque venu d'un autre PC)
filedo vd adopt fdpart:{GUID} as data

# Supprimer la partition: tapez le nom du disque pour confirmer; l'espace redevient non alloué
filedo vd destroy data
```

`<disk>` est `disk:{GUID}` ou le numéro qu'a affiché `vd disks`; `size` est `max` ou une taille comme `40G`
(arrondie vers le bas au MiB, 64 MiB au minimum). Le profil par défaut est `fast`; `plain`, `vault` et `ram`
fonctionnent aussi. Le premier montage formate le volume en NTFS. Un disque sur partition se désigne par son
nom ou par son localisateur `fdpart:{GUID}`, jamais par un chemin; `mount`, `unmount`, `save`, `info`,
`verify`, `export`, `pass`, `seal`, `clone`, `vd auto` et le gardien d'arrêt fonctionnent comme pour un
fichier, `compact` et `grow` non (sa taille est fixe), et le partage par Fast Media Sorter est refusé pour
l'instant. `vd forget` retire le nom et laisse la partition sur son disque; `vd destroy .. wipe` écrase la
partition avant de la supprimer. Un disque refusé se termine avec le code 6, un consentement refusé avec le
code 7, et une disposition modifiée depuis la liste avec le code 5 - sans que rien ne soit modifié.

- **Le consentement administrateur pour chaque lecture, pas seulement pour monter.** Une partition ne s'ouvre
  pas sans droits d'administrateur, donc `info`, `verify`, `export`, `pass`, `seal`, `clone` et `vd image`
  demandent eux aussi le consentement à Windows. `vd image` lève ce besoin: le fichier qu'il écrit est un
  `.fdd` ordinaire que chaque chemin de lecture ouvre sans droits d'administrateur.
- **Pas plus rapide qu'un disque fichier.** Mesuré de bout en bout sur un disque NVMe, un disque sur partition
  était plus lent qu'un fichier `.fdd` sur NTFS. Choisissez-le pour l'espace fixe et séparé, pas pour la
  vitesse.
- **`fast` écrase l'espace libre, `plain` et `vault` non.** Ce que l'espace libre contenait avant reste dans
  les clusters inutilisés d'un disque sur partition `plain` ou `vault` jusqu'à ce que le volume écrive
  par-dessus; `fast` écrase toute la partition à sa création.
- **La Gestion des disques de Windows (`diskmgmt.msc`) affiche toujours la partition et peut la supprimer.**
  La supprimer là supprime le disque qu'elle contient, sans question sur son contenu. Le moyen sûr est le
  `vd destroy` de FileDO: il refuse un disque monté, demande de taper le nom du disque et ne supprime qu'une
  partition dont il a prouvé qu'elle appartient à FileDO.
- **Les disques sur partition ne sont pas disponibles dans la version du Microsoft Store.** Là, `vd disks` le
  dit et chaque commande de partition se termine avec le code 6; utilisez le programme d'installation ou la
  version portable de GitHub.

---

## Fonctionnalités Clés

### **Détection de Fausse Capacité**
- **Test de 100 fichiers** avec 1% de capacité chacun
- **Vérification positionnelle aléatoire** - chaque fichier vérifié à des positions aléatoires uniques
- **Protection contre les contrefaçons sophistiquées** - bat les contrôleurs qui préservent les données à des positions prévisibles
- **Motifs lisibles** - utilise `ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789` pour une détection facile de la corruption
- **Sondage rapide brut** (`probe`) - 32 marqueurs par accès LBA direct, terminé en ~1 min (Admin requis)

### **Test de Performance**
- Mesure de la vitesse réelle de lecture/écriture
- Streaming optimisé pour les gros fichiers
- Suivi du progrès avec calcul ETA
- Tailles de fichiers configurables (1MB à 10GB)

### **Gestion des Doublons de Fichiers**
- **Détection de doublons intégrée** - intégrée dans l'application principale
- **Multiples modes de sélection** (plus ancien/plus récent selon la date de création, alphabétique)
- **Actions flexibles** (supprimer/déplacer les doublons) - chaque fichier fait l'objet d'une question, sauf avec `-y` (ou `--yes`) ; une exécution sans console et sans `-y` est refusée avant de toucher quoi que ce soit
- **Regroupement par SHA-256 et comparaison octet par octet avec la copie conservée** juste avant chaque suppression ou déplacement ; un hachage, en cache ou frais, n'est jamais la seule preuve, et un déplacement ne remplace jamais un fichier existant, y compris vers un autre disque
- **Liens physiques et second nom du même fichier** ne comptent jamais comme doublons ; la racine d'un disque ou d'un partage, Windows, Program Files et le TEMP système exigent une confirmation tapée même avec `-y`, et une analyse ignore Windows et Program Files sauf si elle commence à l'intérieur
- **Mise en cache des hachages** pour des rescans plus rapides - le cache (`hash_cache.json`) se trouve dans `%LOCALAPPDATA%\FileDO\state\`, et un hachage en cache ne sert que tant que la taille, la date de modification, l'identifiant du fichier et le change time du fichier sont inchangés
- **Sauvegarde/chargement des listes de doublons** pour le traitement par lots
- **Architecture modulaire** avec le package fileduplicates dédié

### **Fonctionnalités de Sécurité**
- **Effacement de l'espace libre et de fichiers isolés** pour compliquer la récupération (sans garantie sur les SSD et les disques à copie sur écriture)
- **Opérations de remplissage** avec gestion optimisée des buffers
- **Traitement par lots** pour multiples cibles
- **Historique d'opérations complet** avec journalisation JSON
- **Interruption contextuelle** - support d'annulation gracieuse

---

## Référence des Commandes

### Types de Cibles (Auto-détection)
| Motif | Type | Exemple |
|-------|------|---------|
| `C:`, `D:` | Périphérique | `filedo C: test` |
| `C:\folder` | Dossier | `filedo C:\temp speed 100` |
| `\\server\share` | Réseau | `filedo \\nas\backup test` |
| `file.txt` | Fichier | `filedo document.pdf info` |

### Opérations
| Commande | Objectif | Exemple |
|----------|----------|---------|
| `info` | Afficher informations détaillées | `filedo C: info` |
| `short` | Résumé bref | `filedo D: short` |
| `test` | Détection de fausse capacité | `filedo E: test del` |
| `test N` | Test avec N fichiers (défaut 100) | `filedo D: test 1000` |
| `probe` | Sondage rapide en I/O brut (~1 min, Admin requis) | `filedo D: probe` |
| `speed <taille>` | Test de performance | `filedo C: speed 500` |
| `fill [taille]` | Remplir avec données de test | `filedo D: fill 1000` |
| `clean` | Supprimer fichiers de test | `filedo C: clean` |
| `check-duplicates` | Trouver doublons de fichiers | `filedo C: check-duplicates` |
| `cd [mode] [action]` | Vérifier doublons (forme courte) | `filedo D:\Photos cd old del -y` |
| `from <fichier>` | Exécuter commandes par lots | `filedo from script.txt` |
| `hist` | Afficher historique des opérations | `filedo hist` |

### Modificateurs
| Drapeau | Objectif | Exemple |
|---------|----------|---------|
| `del` | Auto-suppression après opération | `filedo E: test del` |
| `nodel` | Conserver fichiers de test | `filedo C: speed 100 nodel` |
| `short` | Sortie brève seulement | `filedo D: speed 100 short` |
| `max` | Taille maximale (10GB) | `filedo C: speed max` |
| `old` | Garder le plus récent comme original (pour cd) | `filedo D:\Photos cd old del` |
| `new` | Garder le plus ancien comme original (pour cd) | `filedo E:\Photos cd new move F:\Dups` |
| `abc` | Garder le dernier alphabétiquement (pour cd) | `filedo C: cd abc` |
| `xyz` | Garder le premier alphabétiquement (pour cd) | `filedo C: cd xyz list dups.lst` |
| `-y`, `--yes` | Supprimer/déplacer les doublons sans question par fichier (pour cd, requis sans console) | `filedo D:\Photos cd old del -y` |

---

## Application GUI

**FileDO GUI** (`filedo_win.exe`) - shell Windows Forms VB.NET : à gauche une colonne de tâches, et pour chaque tâche une page numérotée - ce qu'on traite, quels paramètres, puis vérification et exécution - plus une page **«Commande»**, le constructeur expert, qui assemble et exécute n'importe quelle commande FileDO :

- **Sélection visuelle de cible** avec boutons radio (Périphérique/Dossier/Réseau/Fichier)
- **Menu déroulant d'opérations** sur la page «Commande» (Info, Vitesse, Remplissage, Test, Nettoyage, Vérification des doublons)
- **Une page par tâche** dans la fenêtre par défaut - capacité, vitesse, informations, vérification des fichiers endommagés, sondage direct, récupération, doublons, comparaison, nettoyage, copie, remplissage, effacement et les trois tâches des fichiers secrets - chacune portant toutes les options que la CLI accepte pour elle, y compris les vingt-neuf options de `check`
- **Saisie de paramètres** avec validation
- **Aperçu de commande en temps réel** sur la page «Commande», montrant la commande CLI équivalente
- **Bouton parcourir** pour sélection facile du chemin
- **Suivi du progrès** avec sortie en temps réel
- **Exécution en un clic** avec affichage de la sortie
- **Page «À propos»** : version, auteur et liens vers le site, le code source, le suivi des problèmes, la page de confidentialité et les autres outils de l'auteur
- **Envoyer les logs à l'auteur** - l'unique bouton de cette page regroupe les logs FileDO trouvés sur cette machine dans un seul zip, ouvre le dossier avec le fichier sélectionné, place son chemin dans le presse-papiers et ouvre votre programme de messagerie avec l'adresse et l'objet déjà remplis. Vous voyez d'abord le zip, et rien n'est envoyé avant que vous envoyiez le message vous-même

```bash
# Lancer depuis le dossier filedo_win_vb
filedo_win.exe          # Interface Windows GUI
```

Les pages **«Protéger»** traitent les fichiers secrets `.fd-sec` - rendre un fichier secret, récupérer l'original, ou l'ouvrir sans le décompresser. Le mot de passe est masqué et n'entre jamais dans une ligne de commande. Donner directement un chemin `.fd-sec` à la fenêtre l'ouvre sur la page de ce conteneur (`filedo_win.exe C:\a\x.fd-sec`); un double-clic dans l'Explorateur ne passe pas du tout par la fenêtre - il exécute **Unsecure and start** dans la console, comme décrit plus haut. «Historique», «Réglages» et «À propos» ont leurs propres pages; l'ancienne fenêtre constructeur de commandes est retirée, et une ligne de commande écrite à la main se saisit sur la page **«Commande»**. Le **Gestionnaire de disques** - une seconde fenêtre pour les disques virtuels, ouverte par **Ctrl+Shift+D**, le bouton en haut à droite ou `filedo_win.exe --disks` - est décrit dans la section *Disques virtuels* plus haut.

**Fonctionnalités :**
- Construit avec VB.NET Windows Forms pour une expérience native Windows
- Validation automatique des commandes et vérification des paramètres
- Affichage de sortie en temps réel avec codage couleur
- Intégration avec l'application CLI principale

---

## Fonctionnalités Avancées

### Traitement par Lots
Créez `commands.txt` :
```text
# Vérifier plusieurs périphériques
device C: info
device D: test del
device E: speed 100
folder C:\temp clean
```

Exécutez : `filedo from commands.txt`

Codes de sortie (toutes les commandes hors conteneur) : **0 passed or done, 1 ran and found a defect, 2 could not be verified.**

### Suivi d'Historique
```bash
filedo hist              # Afficher les 10 dernières opérations
filedo history           # Afficher l'historique des commandes
# L'historique est automatiquement maintenu pour toutes les opérations
```

### Support d'Interruption
```bash
# Toutes les opérations longues supportent l'interruption Ctrl+C
# Annulation gracieuse avec nettoyage
# Interruption contextuelle aux points optimaux
```

### Opérations Réseau
```bash
# Partages SMB et lecteurs réseau
filedo \\server\backup speed 100
filedo \\nas\storage test del
filedo network \\pc\share info
```

### Comparaison de dossiers & Nettoyage

`compare <source> <target> --strict` vérifie que les arborescences correspondent : mêmes chemins relatifs, tailles et heures de modification donnent `Passed` (code 0) ; une différence donne `Failed` (code 1) ; une comparaison incomplète donne `Not proven` (code 2). Ajoutez `--by-hash` pour comparer les paires de même taille par contenu plutôt que par date. Un simple `compare` signale toujours les différences avec `Done` (code 0) ; `--strict` ne peut pas être combiné avec une suppression. Les deux modes publient `onlyInSource`, `onlyInTarget`, `differentFiles`, `sameFiles`, `totalSource` et `totalTarget` dans le résultat de `--events`. Pour `check`, `--max-files N` lit au maximum N fichiers (0 signifie sans limite) ; `--resume` se termine avec `Passed` (code 0) lorsque chaque fichier admissible possède une entrée inchangée dans la liste des fichiers bien lus : `checkedFiles = 0`, et `skippedGoodFiles` compte les fichiers vérifiés auparavant.

```bash
# Comparer deux dossiers et enregistrer un rapport
filedo compare D:\Data E:\Backup

# Comparer et supprimer (permanent, sans confirmation)
filedo cmp D:\Data E:\Backup del source  # supprimer dans Source si présent dans Target
filedo cmp D:\Data E:\Backup del target  # supprimer dans Target si présent dans Source
filedo cmp D:\Data E:\Backup del old     # supprimer le plus ancien (mtime), égal: ignorer
filedo cmp D:\Data E:\Backup del new     # supprimer le plus récent (mtime), égal: ignorer
filedo cmp D:\Data E:\Backup del small   # supprimer le plus petit, égal: ignorer
filedo cmp D:\Data E:\Backup del big     # supprimer le plus grand, égal: ignorer
 
# Qualificateur de côté (facultatif)
filedo cmp D:\Data E:\Backup del small source  # seulement si le plus petit est côté Source
filedo cmp D:\Data E:\Backup del big target    # seulement si le plus grand est côté Target
filedo cmp D:\Data E:\Backup del old target    # seulement si le plus ancien est côté Target
filedo cmp D:\Data E:\Backup del new source    # seulement si le plus récent est côté Source
filedo cmp D:\Data E:\Backup del source --yes  # sans question (scripts); les vérifications restent
```

Notes: appariement par chemin relatif; `del source` et `del target` ne suppriment une paire que si la taille et l'heure de modification concordent (`--by-hash` : même contenu ; `--allow-mismatch` : toute paire) - une paire qui diffère est signalée et conservée; deux écritures du même dossier, ou un dossier contenu dans l'autre, sont refusées; chaque règle de suppression indique d'abord le nombre de fichiers et le mode, puis demande `(y/N)` - `--yes` (ou `-y`) saute la question, jamais une vérification, et sans réponse (stdin fermé, script) rien n'est supprimé et le code de sortie est 2; mtime pour old/new; Windows insensible à la casse, la suppression utilise le vrai nom du fichier; ce qui n'a pu être lu ou supprimé termine avec le code 2; logs: compare_report_*.log, delete_report_<mode>_*.log.

---

## Notes Importantes

> **Détection de Fausse Capacité** : Crée 100 fichiers (1% de capacité chacun) avec **support d'interruption contextuelle**. Utilise des motifs de vérification aléatoires modernes et une gestion optimisée des buffers pour une détection fiable.

> **Interruption Améliorée** : Toutes les opérations longues supportent **l'annulation gracieuse Ctrl+C** avec nettoyage automatique. Vérifications d'interruption contextuelle aux points optimaux pour une réactivité immédiate.

> **Effacement** : `fill <taille> del` écrase l'espace libre avec gestion optimisée des buffers et écriture contextuelle, pour compliquer la récupération.

> **Fichiers de Test** : Crée des fichiers `FILL_*.tmp` et `speedtest_*.txt`. `clean` supprime seulement ceux que FileDO a écrits - avec ses noms et son contenu, rien d'autre - après les avoir listés et avoir demandé ; `--yes` répond à la question.

> **Packages partagés** : `fileduplicates` (détection des doublons), `fsx` (identité des chemins et écriture atomique sans remplacement) et `statedir` (la racine d'état, `%LOCALAPPDATA%\FileDO\state`).

---

## Exemples par Cas d'Usage

<details>
<summary><b>Vérification d'Authenticité USB/Carte SD</b></summary>

```bash
# Test rapide avec nettoyage
filedo E: test del

# Test détaillé, garder fichiers pour analyse
filedo F: test

# Vérifier d'abord les infos du disque
filedo E: info
```
</details>

<details>
<summary><b>Benchmark de Performance</b></summary>

```bash
# Test rapide 100MB
filedo C: speed 100 short

# Test de performance maximum (10GB)
filedo D: speed max

# Test de vitesse réseau
filedo \\server\backup speed 500
```
</details>

<details>
<summary><b>Effacement</b></summary>

```bash
# Remplir 5GB puis supprimer les fichiers de test
filedo C: fill 5000 del

# Nettoyer les fichiers de test existants
filedo D: clean

# Avant mise au rebut du disque
filedo E: fill max del
```
</details>

<details>
<summary><b>Recherche et Gestion des Doublons</b></summary>

```bash
# Trouver doublons dans le répertoire courant
filedo . check-duplicates

# Trouver et supprimer anciens doublons (avec une question par fichier)
filedo D:\Photos cd old del

# La même chose sans question par fichier
filedo D:\Photos cd old del -y

# Trouver et déplacer nouveaux doublons vers sauvegarde
filedo E:\Photos cd new move E:\Backup

# Sauvegarder liste de doublons pour traitement ultérieur
filedo D: cd list duplicates.lst

# Traiter liste sauvegardée avec action spécifique
filedo cd from list duplicates.lst xyz del
```
</details>

---

## Détails Techniques

### Architecture
- **Design Modulaire** : Séparé en packages spécialisés pour une meilleure maintenabilité
- **Opérations Contextuelles** : Toutes les opérations longues supportent l'annulation gracieuse
- **Interface Unifiée** : Interface commune `Tester` pour tous les types de stockage
- **Optimisation Mémoire** : Opérations de streaming avec gestion optimisée des buffers
- **Multi-plateforme** : Support principal Windows avec base de code Go portable

### Structure des Packages
```
FileDO/
├── main.go                    # Point d'entrée de l'application
├── fsx/                      # Identité des chemins et écriture atomique
├── statedir/                 # Emplacement des fichiers d'état (%LOCALAPPDATA%\FileDO\state)
├── fileduplicates/           # Gestion des doublons de fichiers
│   ├── types.go              # Interfaces de détection de doublons
│   ├── duplicates.go         # Logique principale des doublons
│   ├── duplicates_impl.go    # Détails d'implémentation
│   └── worker.go             # Traitement en arrière-plan
├── filedo_win_vb/           # Application GUI VB.NET
│   ├── FileDOGUI.sln        # Solution Visual Studio
│   ├── Program.vb           # Point d'entrée; la fenêtre est ShellForm.vb
│   └── bin/                 # Exécutable GUI compilé
├── command_handlers.go       # Gestion des commandes
├── device_windows.go         # Opérations sur périphériques
├── folder.go                 # Opérations sur dossiers
├── network_windows.go        # Opérations sur stockage réseau
├── interrupt.go              # Gestion des interruptions
├── progress.go               # Suivi du progrès
├── main_types.go             # Définitions de types héritées
├── history.json              # Historique des opérations
└── hash_cache.json           # Cache des hachages pour doublons
```

### Fonctionnalités Clés
- **InterruptHandler Amélioré** : Interruption thread-safe avec support contextuel
- **Gestion Optimisée des Buffers** : Redimensionnement dynamique des buffers pour performance optimale
- **Tests Complets** : Détection de fausse capacité avec vérification aléatoire
- **Détection de Doublons** : Comparaison de fichiers basée sur SHA-256 avec mise en cache, et vérification octet par octet avant toute suppression ou tout déplacement
- **Traitement par Lots** : Exécution de scripts avec gestion d'erreurs
- **Tenue d'Historique** : Suivi d'opérations basé sur JSON

---

## Historique des Versions

**v2610070412** (Actuelle)
- **Disques virtuels (`.fdd`)**: un volume entier dans un fichier, monté comme lettre de lecteur - `filedo vd new`, `mount`, `unmount`, plus `info`, `verify` et `export` (image brute ou VHD) sans montage, et `grow`, `compact`, `format`, `seal`, `clone`, `pass`, `destroy`; camouflé sans mot de passe, chiffré avec, et jamais l'un appelé l'autre
- **Disques sur partition**: un disque virtuel peut vivre dans une nouvelle partition GPT prise sur l'espace libre d'un disque au lieu d'un fichier - `filedo vd disks` liste chaque disque, son espace libre et pourquoi une zone est utilisable ou non; `vd new part`, `vd image` (copie dans un `.fdd` ordinaire) et `vd adopt`; rien en dehors de l'espace libre n'est touché, Windows demande l'accord d'un administrateur à chaque lecture, et la version du Microsoft Store ne les propose pas
- **Vérification et sécurité**: `chkdsk` vérifie le volume d'un conteneur non monté sans rien écrire, `chkdsk fix` répare un conteneur qui n'a pas été fermé proprement; `vd guard on` enregistre les disques `ram` et démonte proprement chaque conteneur à la fin de votre session; `vd auto` monte un conteneur camouflé à l'ouverture de votre session
- **Partage**: en option, `vd share` propose un disque aux téléphones et tablettes associés à Fast Media Sorter for Windows comme un dossier de plus (`vd open`, `vd close`, `vd autostart`, ou *Partager via Fast Media Sorter & Sharing..* dans le Gestionnaire de disques); ce programme sert les fichiers en SFTP, FileDO n'ouvre lui-même aucune connexion réseau, et un disque chiffré est déverrouillé sur ce PC, jamais depuis le téléphone
- **Explorateur**: la nouvelle fonctionnalité *Disk container files (.fdd)* du setup (`DiskContainerIntegration`) donne à `.fdd` une icône, un double-clic qui monte et *Mount read-only* / *Unmount*; sans programme d'installation, `filedo vd register` fait de même
- **GUI**: un nouveau groupe **Disques** - une page par opération - et le **Gestionnaire de disques**, une seconde fenêtre avec une ligne par disque virtuel, son état en toutes lettres, et monter, démonter, ouvrir et enregistrer depuis la ligne; un montage survit à la fenêtre, et le programme d'installation ajoute l'entrée *FileDO Disk Manager* au menu Démarrer, qui l'ouvre directement
- **Limites**: Windows uniquement; monter demande l'accord d'un administrateur; la version du Microsoft Store lit, vérifie et exporte les conteneurs mais ne peut ni les monter, ni utiliser les disques sur partition, ni les partager
- **Documentation**: nouveaux guides - disques virtuels et partage de disques via Fast Media Sorter; pages du site en allemand et en français, et une page des nouveautés

**v2609241700** (Précédente)
- **Fichiers secrets (`.fd-sec`)** : un fichier est placé dans un conteneur protégé par mot de passe puis restauré - `secure`, `unsecure`, `reveal` - en ligne de commande, depuis le menu de l'Explorateur ou les pages Protect de la fenêtre ; le vrai nom, la taille et les dates de l'original sont scellés à l'intérieur
- **Intégration à l'Explorateur** : le setup ajoute le groupe `File DO..` au menu contextuel (Secure, Unsecure, Wipe this file, Check this file, Info) et le type de document `.fd-sec` ; `filedo fdsec register` / `unregister` fait de même sans installateur
- **GUI** : une nouvelle fenêtre - la liste des tâches à gauche, une page par tâche avec toutes les options de la CLI, plus les pages Command, History, Settings et About ; l'ancienne fenêtre de construction de commandes est retirée
- **Copie** : elle commence dès le premier fichier ; `--precount` compte d'abord l'arborescence pour des totaux et une durée restante exacts
- **CLI** : des codes de sortie uniques - 0 réussi ou terminé, 1 défaut trouvé, 2 vérification impossible ; les identifiants sont masqués avant toute écriture dans l'historique
- **Distribution** : FileDO est sur le Microsoft Store ; `THIRD-PARTY-NOTICES.txt` est inclus dans le zip, le MSI et le paquet Store
- **Documentation** : nouveaux guides - fichiers secrets et "Windows warned you about FileDO"

**v2607301014**
- **GUI** : interface en 5 langues (anglais, russe, ukrainien, allemand, français) avec changement de langue à la volée et icône d'application
- **GUI** : fenêtre "À propos" avec envoi des journaux à l'auteur
- **Confidentialité** : page de confidentialité publiée, liée depuis le site et la fiche du Store
- **CLI** : nouveau slogan "pleasantly paranoid" et texte d'aide plus clair
- **Documentation** : site repensé avec de nouveaux guides pas à pas (vérification du stockage, fichiers et copies, constructeur de commandes GUI)
- **Projet** : sources réorganisées sous `cmd/` (filedo, filedo-check, filedo-fill, filedo-test) ; flux de build et de release séparés

**v2606120121**
- **Wipe** : contrôles de sécurité renforcés pour l'effacement
- **Doublons** : correction de l'exactitude et du parallélisme du cache de doublons
- **Copie** : parcours de répertoires allégés lors de la copie

**v2605152056**
- **Installateur** : setup EXE (un bundle WiX contenant le MSI) et le MSI nu ; icône d'application intégrée dans tous les EXE et l'entrée des programmes installés
- **Store** : spécification de publication sur le Microsoft Store et image d'aperçu

**v2604272228**
- **Distribution** : première publication sur winget (SerZhyAle.FileDO) et pipeline de release GitHub Actions
- **Binaires** : informations de version PE et manifeste d'application Windows intégrés dans tous les binaires
- **Documentation** : pages GitHub Pages repensées et simplifiées

**v2507112115**
- **Refactorisation majeure** : Logique de test de capacité extraite dans un package `capacitytest` dédié
- **Interruption améliorée** : Ajout d'annulation contextuelle avec `InterruptHandler` thread-safe
- **Performance améliorée** : Algorithmes de gestion des buffers et de vérification optimisés
- **Meilleure architecture** : Design modulaire avec séparation claire des responsabilités
- **GUI VB.NET** : Application Windows Forms mise à jour avec meilleure intégration

**v2507082120**
- Ajout de détection et gestion des doublons de fichiers
- Multiples modes de sélection de doublons (old/new/abc/xyz)
- Mise en cache des hachages pour scan de doublons plus rapide
- Support de sauvegarde/chargement des listes de doublons
- Application GUI avec fonctionnalités de gestion des doublons

**v2507062220** (Ancienne)
- Système de vérification amélioré avec vérification multi-position
- Motifs de texte lisibles pour détection de corruption
- Affichage de progrès amélioré et mécanismes de protection
- Corrections de bugs et améliorations de gestion d'erreurs

---

<div align="center">

**FileDO v2610070412** - Outil Avancé pour Fichiers et Stockage

Créé par **sza@ukr.net** | [Licence MIT](LICENSE) | [Dépôt GitHub](https://github.com/SerZhyAle/FileDO) | [Universal Agent Kit](https://serzhyale.github.io/universal-agent-kit/)

---

### Dernières Améliorations

- **Architecture Modulaire** : Refactorisée en packages spécialisés (`capacitytest`, `fileduplicates`)
- **Interruption Améliorée** : Annulation contextuelle avec nettoyage gracieux
- **Opérations Thread-Safe** : `InterruptHandler` amélioré avec protection par mutex
- **Meilleure Performance** : Algorithmes optimisés de gestion des buffers et de vérification
- **GUI Mis à Jour** : Application Windows Forms VB.NET avec intégration améliorée

</div>
