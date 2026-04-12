# FileDO - Outil Avancé pour Fichiers et Stockage

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/SerZhyAle/FileDO)](https://goreportcard.com/report/github.com/SerZhyAle/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2507112115-blue.svg)](https://github.com/SerZhyAle/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/SerZhyAle/FileDO)

**🔍 Test de Stockage • 🚀 Analyse de Performance • 🛡️ Suppression Sécurisée • 🎯 Détection de Fausse Capacité • 📁 Gestion des Doublons**

</div>

---

## 🎯 Démarrage Rapide

### ⚡ Tâches Les Plus Courantes

```bash
# Vérifier la contrefaçon d'une clé USB/carte SD
filedo E: test del

# Test de performance du disque
filedo C: speed 100

# Nettoyage sécurisé de l'espace libre
filedo D: fill 1000 del

# Recherche et gestion des doublons
filedo C: check-duplicates
filedo D: cd old del

# Copie avec suivi de progression
filedo folder C:\Source copy D:\Backup
filedo device E: copy F:\Archive

# Nettoyage rapide des dossiers
filedo folder C:\Temp wipe
filedo folder D:\Cache w

# Afficher les informations du disque
filedo C: info
```

### 📥 Installation

1. **Télécharger**: Obtenez `filedo.exe` depuis les releases
2. **Version GUI**: Téléchargez aussi `filedo_win.exe` pour l'interface graphique (VB.NET)
3. **Exécution**: Lancez depuis la ligne de commande ou via l'interface graphique

---

## 🔧 Opérations Principales

<table>
<tr>
<td width="50%">

### 💾 Test de Périphériques
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

### 📁 Opérations Fichiers et Dossiers
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

## 🌟 Fonctionnalités Clés

### 🎯 **Détection de Fausse Capacité**
- **Test de 100 fichiers** avec 1% de capacité chacun
- **Vérification positionnelle aléatoire** - chaque fichier vérifié à des positions aléatoires uniques
- **Protection contre les contrefaçons sophistiquées** - bat les contrôleurs qui préservent les données à des positions prévisibles
- **Motifs lisibles** - utilise `ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789` pour une détection facile de la corruption
- **Sondage rapide brut** (`probe`) - 32 marqueurs par accès LBA direct, terminé en ~1 min (Admin requis)

### ⚡ **Test de Performance**
- Mesure de la vitesse réelle de lecture/écriture
- Streaming optimisé pour les gros fichiers
- Suivi du progrès avec calcul ETA
- Tailles de fichiers configurables (1MB à 10GB)

### 🔍 **Gestion des Doublons de Fichiers**
- **Détection de doublons intégrée** - intégrée dans l'application principale
- **Multiples modes de sélection** (plus ancien/plus récent/alphabétique)
- **Actions flexibles** (supprimer/déplacer les doublons)
- **Identification fiable basée sur MD5**
- **Mise en cache des hachages** pour des rescans plus rapides
- **Sauvegarde/chargement des listes de doublons** pour le traitement par lots
- **Architecture modulaire** avec le package fileduplicates dédié

### 🛡️ **Fonctionnalités de Sécurité**
- **Suppression sécurisée de données haute vitesse** pour empêcher la récupération
- **Opérations de remplissage** avec gestion optimisée des buffers
- **Traitement par lots** pour multiples cibles
- **Historique d'opérations complet** avec journalisation JSON
- **Interruption contextuelle** - support d'annulation gracieuse

---

## 💻 Référence des Commandes

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
| `cd [mode] [action]` | Vérifier doublons (forme courte) | `filedo C: cd old del` |
| `from <fichier>` | Exécuter commandes par lots | `filedo from script.txt` |
| `hist` | Afficher historique des opérations | `filedo hist` |

### Modificateurs
| Drapeau | Objectif | Exemple |
|---------|----------|---------|
| `del` | Auto-suppression après opération | `filedo E: test del` |
| `nodel` | Conserver fichiers de test | `filedo C: speed 100 nodel` |
| `short` | Sortie brève seulement | `filedo D: speed 100 short` |
| `max` | Taille maximale (10GB) | `filedo C: speed max` |
| `old` | Garder le plus récent comme original (pour cd) | `filedo D: cd old del` |
| `new` | Garder le plus ancien comme original (pour cd) | `filedo E: cd new move F:` |
| `abc` | Garder le dernier alphabétiquement (pour cd) | `filedo C: cd abc` |
| `xyz` | Garder le premier alphabétiquement (pour cd) | `filedo C: cd xyz list dups.lst` |

---

## 🖥️ Application GUI

**FileDO GUI** (`filedo_win.exe`) - Application Windows Forms VB.NET fournit une interface conviviale :

- ✅ **Sélection visuelle de cible** avec boutons radio (Périphérique/Dossier/Réseau/Fichier)
- ✅ **Menu déroulant d'opérations** (Info, Vitesse, Remplissage, Test, Nettoyage, Vérification des doublons)
- ✅ **Saisie de paramètres** avec validation
- ✅ **Aperçu de commande en temps réel** montrant la commande CLI équivalente
- ✅ **Bouton parcourir** pour sélection facile du chemin
- ✅ **Suivi du progrès** avec sortie en temps réel
- ✅ **Exécution en un clic** avec affichage de la sortie

```bash
# Lancer depuis le dossier filedo_win_vb
filedo_win.exe          # Interface Windows GUI
```

**Fonctionnalités :**
- Construit avec VB.NET Windows Forms pour une expérience native Windows
- Validation automatique des commandes et vérification des paramètres
- Affichage de sortie en temps réel avec codage couleur
- Intégration avec l'application CLI principale

---

## 🔍 Fonctionnalités Avancées

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

Notes: appariement par chemin relatif; égalité par taille seulement; mtime pour old/new; Windows insensible à la casse; logs: compare_report_*.log, delete_report_<mode>_*.log.

---

## ⚠️ Notes Importantes

> **🎯 Détection de Fausse Capacité** : Crée 100 fichiers (1% de capacité chacun) avec **support d'interruption contextuelle**. Utilise des motifs de vérification aléatoires modernes et une gestion optimisée des buffers pour une détection fiable.

> **🔥 Interruption Améliorée** : Toutes les opérations longues supportent **l'annulation gracieuse Ctrl+C** avec nettoyage automatique. Vérifications d'interruption contextuelle aux points optimaux pour une réactivité immédiate.

> **🛡️ Suppression Sécurisée** : `fill <taille> del` écrase l'espace libre avec gestion optimisée des buffers et écriture contextuelle pour la suppression sécurisée des données.

> **🟢 Fichiers de Test** : Crée des fichiers `FILL_*.tmp` et `speedtest_*.txt`. Utilisez la commande `clean` pour leur suppression automatique.

> **🔵 Architecture Modulaire** : Refactorisée avec des packages séparés `capacitytest` et `fileduplicates` pour une meilleure maintenabilité et extensibilité.

---

## 📖 Exemples par Cas d'Usage

<details>
<summary><b>🔍 Vérification d'Authenticité USB/Carte SD</b></summary>

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
<summary><b>⚡ Benchmark de Performance</b></summary>

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
<summary><b>🛡️ Suppression Sécurisée de Données</b></summary>

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
<summary><b>🔍 Recherche et Gestion des Doublons</b></summary>

```bash
# Trouver doublons dans le répertoire courant
filedo . check-duplicates

# Trouver et supprimer anciens doublons
filedo C: cd old del

# Trouver et déplacer nouveaux doublons vers sauvegarde
filedo E: cd new move E:\Backup

# Sauvegarder liste de doublons pour traitement ultérieur
filedo D: cd list duplicates.lst

# Traiter liste sauvegardée avec action spécifique
filedo cd from list duplicates.lst xyz del
```
</details>

---

## 🏗️ Détails Techniques

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
├── capacitytest/             # Module de test de capacité
│   ├── types.go              # Interfaces et types principaux
│   ├── test.go               # Logique de test principale
│   └── utils.go              # Utilitaires et fonctions de vérification
├── fileduplicates/           # Gestion des doublons de fichiers
│   ├── types.go              # Interfaces de détection de doublons
│   ├── duplicates.go         # Logique principale des doublons
│   ├── duplicates_impl.go    # Détails d'implémentation
│   └── worker.go             # Traitement en arrière-plan
├── filedo_win_vb/           # Application GUI VB.NET
│   ├── FileDOGUI.sln        # Solution Visual Studio
│   ├── MainForm.vb          # Logique du formulaire principal
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
- **Détection de Doublons** : Comparaison de fichiers basée sur MD5 avec mise en cache
- **Traitement par Lots** : Exécution de scripts avec gestion d'erreurs
- **Tenue d'Historique** : Suivi d'opérations basé sur JSON

---

## 🔄 Historique des Versions

**v2507112115** (Actuelle)
- **Refactorisation majeure** : Logique de test de capacité extraite dans un package `capacitytest` dédié
- **Interruption améliorée** : Ajout d'annulation contextuelle avec `InterruptHandler` thread-safe
- **Performance améliorée** : Algorithmes de gestion des buffers et de vérification optimisés
- **Meilleure architecture** : Design modulaire avec séparation claire des responsabilités
- **GUI VB.NET** : Application Windows Forms mise à jour avec meilleure intégration

**v2507082120** (Précédente)
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

**FileDO v2507112115** - Outil Avancé pour Fichiers et Stockage

Créé par **sza@ukr.net** | [Licence MIT](LICENSE) | [Dépôt GitHub](https://github.com/SerZhyAle/FileDO)

---

### 🚀 Dernières Améliorations

- **🔧 Architecture Modulaire** : Refactorisée en packages spécialisés (`capacitytest`, `fileduplicates`)
- **⚡ Interruption Améliorée** : Annulation contextuelle avec nettoyage gracieux
- **🛡️ Opérations Thread-Safe** : `InterruptHandler` amélioré avec protection par mutex
- **📊 Meilleure Performance** : Algorithmes optimisés de gestion des buffers et de vérification
- **🖥️ GUI Mis à Jour** : Application Windows Forms VB.NET avec intégration améliorée

</div>
