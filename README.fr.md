# FileDO - Outil Avancé pour Fichiers et Stockage

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/SerZhyAle/FileDO)](https://goreportcard.com/report/github.com/SerZhyAle/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2609241700-blue.svg)](https://github.com/SerZhyAle/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/SerZhyAle/FileDO)

**Test de Stockage • Analyse de Performance • Suppression Sécurisée • Détection de Fausse Capacité • Gestion des Doublons**

</div>

---

## Démarrage Rapide

### Tâches Les Plus Courantes

```bash
# Vérifier la contrefaçon d'une clé USB/carte SD
filedo E: test del

# Test de performance du disque
filedo C: speed 100

# Nettoyage sécurisé de l'espace libre
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
- crée une **entrée dans le menu Démarrer** et une **icône sur le bureau** pour la fenêtre FileDO (`filedo_win.exe`);
- enregistre l'**intégration à l'Explorateur**: un groupe `File DO..` dans le menu contextuel de tout fichier - Secure (garder, supprimer ou effacer l'original, ou un nom de conteneur aléatoire), Unsecure (en option, supprimer le conteneur ou démarrer aussitôt le fichier restauré), Wipe this file, Check this file, Info - ainsi que le type de document `.fd-sec` avec sa propre icône: le double-clic est exactement l'entrée Unsecure and start: une console y demande le mot de passe, restaure l'original sous son vrai nom dans `%LOCALAPPDATA%\FileDO\reveal` - un dossier que seuls ce compte et le système peuvent lire -, le confie au programme auquel appartient sa véritable extension, puis retire cette copie à la fermeture de la fenêtre de console. Sous Windows 11, le groupe se trouve sous *Afficher d'autres options*.

Le programme d'installation n'est pas signé numériquement: au premier lancement, Windows peut afficher *Windows a protégé votre ordinateur* puis demander les droits d'administrateur. Comparez le SHA256 (le fichier `.sha256` publié à côté du téléchargement; `certutil -hashfile FileDO-<version>-setup.exe SHA256`), puis choisissez *Informations complémentaires* et *Exécuter quand même*. Pourquoi l'avertissement apparaît, à quoi servent les droits d'administrateur et ce que FileDO ne fait jamais: [Windows warned you about FileDO](https://serzhyale.github.io/FileDO/guides/install-trust.html) (page en EN/RU/UA).

Ces deux derniers points sont des fonctionnalités que l'on peut décocher sur la page « Customize » du programme d'installation, puis activer ou désactiver plus tard via **Modifier** dans « Applications et fonctionnalités ». Sans surveillance:

```powershell
FileDO-<version>-setup.exe /quiet
FileDO-<version>-setup.exe /uninstall
```

La même release publie aussi le `FileDO-<version>-windows-x64.msi` nu - c'est exactement ce fichier qui se trouve dans le setup EXE - pour les outils de déploiement qui veulent le paquet et les noms de fonctionnalités directement:

```powershell
msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main,ExplorerIntegration,DesktopShortcut
msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main
```

La désinstallation retire tout ce que le programme d'installation a écrit, y compris les entrées de registre.

#### Option 3 - Microsoft Store (MSIX)

Un paquet, deux entrées: la tuile **FileDO** et la commande `filedo` dans le `PATH`. La version du Store n'a **pas** les entrées de l'Explorateur: un paquet ne peut les obtenir que via un gestionnaire shell signé, ce qui est un travail distinct. Elle prend **bien** en charge le type de fichier `.fd-sec`: un double-clic sur un conteneur ouvre la fenêtre FileDO sur la page *Ouvrir un fichier secret* avec ce conteneur déjà choisi, et le mot de passe y est demandé.

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
- **Suppression sécurisée de données haute vitesse** pour empêcher la récupération
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

Les pages **«Protéger»** traitent les fichiers secrets `.fd-sec` - rendre un fichier secret, récupérer l'original, ou l'ouvrir sans le décompresser. Le mot de passe est masqué et n'entre jamais dans une ligne de commande. Donner directement un chemin `.fd-sec` à la fenêtre l'ouvre sur la page de ce conteneur (`filedo_win.exe C:\a\x.fd-sec`); un double-clic dans l'Explorateur ne passe pas du tout par la fenêtre - il exécute **Unsecure and start** dans la console, comme décrit plus haut. «Historique», «Réglages» et «À propos» ont leurs propres pages; l'ancienne fenêtre constructeur de commandes est retirée, et une ligne de commande écrite à la main se saisit sur la page **«Commande»**.

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
```

Notes: appariement par chemin relatif; `del source` et `del target` ne suppriment une paire que si la taille et l'heure de modification concordent (`--by-hash` : même contenu ; `--allow-mismatch` : toute paire) - une paire qui diffère est signalée et conservée; deux écritures du même dossier, ou un dossier contenu dans l'autre, sont refusées; mtime pour old/new; Windows insensible à la casse, la suppression utilise le vrai nom du fichier; ce qui n'a pu être lu ou supprimé termine avec le code 2; logs: compare_report_*.log, delete_report_<mode>_*.log.

---

## Notes Importantes

> **Détection de Fausse Capacité** : Crée 100 fichiers (1% de capacité chacun) avec **support d'interruption contextuelle**. Utilise des motifs de vérification aléatoires modernes et une gestion optimisée des buffers pour une détection fiable.

> **Interruption Améliorée** : Toutes les opérations longues supportent **l'annulation gracieuse Ctrl+C** avec nettoyage automatique. Vérifications d'interruption contextuelle aux points optimaux pour une réactivité immédiate.

> **Suppression Sécurisée** : `fill <taille> del` écrase l'espace libre avec gestion optimisée des buffers et écriture contextuelle pour la suppression sécurisée des données.

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
<summary><b>Suppression Sécurisée de Données</b></summary>

```bash
# Remplir 5GB puis supprimer de façon sécurisée
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

**v2609241700** (Actuelle)
- **Fichiers secrets (`.fd-sec`)** : un fichier est placé dans un conteneur protégé par mot de passe puis restauré - `secure`, `unsecure`, `reveal` - en ligne de commande, depuis le menu de l'Explorateur ou les pages Protect de la fenêtre ; le vrai nom, la taille et les dates de l'original sont scellés à l'intérieur
- **Intégration à l'Explorateur** : le setup ajoute le groupe `File DO..` au menu contextuel (Secure, Unsecure, Wipe this file, Check this file, Info) et le type de document `.fd-sec` ; `filedo fdsec register` / `unregister` fait de même sans installateur
- **GUI** : une nouvelle fenêtre - la liste des tâches à gauche, une page par tâche avec toutes les options de la CLI, plus les pages Command, History, Settings et About ; l'ancienne fenêtre de construction de commandes est retirée
- **Copie** : elle commence dès le premier fichier ; `--precount` compte d'abord l'arborescence pour des totaux et une durée restante exacts
- **CLI** : des codes de sortie uniques - 0 réussi ou terminé, 1 défaut trouvé, 2 vérification impossible ; les identifiants sont masqués avant toute écriture dans l'historique
- **Distribution** : FileDO est sur le Microsoft Store ; `THIRD-PARTY-NOTICES.txt` est inclus dans le zip, le MSI et le paquet Store
- **Documentation** : nouveaux guides - fichiers secrets et "Windows warned you about FileDO"

**v2607301014** (Précédente)
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

**FileDO v2609241700** - Outil Avancé pour Fichiers et Stockage

Créé par **sza@ukr.net** | [Licence MIT](LICENSE) | [Dépôt GitHub](https://github.com/SerZhyAle/FileDO) | [Universal Agent Kit](https://serzhyale.github.io/universal-agent-kit/)

---

### Dernières Améliorations

- **Architecture Modulaire** : Refactorisée en packages spécialisés (`capacitytest`, `fileduplicates`)
- **Interruption Améliorée** : Annulation contextuelle avec nettoyage gracieux
- **Opérations Thread-Safe** : `InterruptHandler` amélioré avec protection par mutex
- **Meilleure Performance** : Algorithmes optimisés de gestion des buffers et de vérification
- **GUI Mis à Jour** : Application Windows Forms VB.NET avec intégration améliorée

</div>
