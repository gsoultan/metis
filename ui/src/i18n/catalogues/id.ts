/**
 * Bahasa Indonesia.
 *
 * A second catalogue exists so the machinery is exercised by something real
 * rather than by a copy of English: a language with no plural inflection proves
 * that the plural forms come from `Intl.PluralRules` and not from an assumption
 * baked into the code. Indonesian has one form, so `other` carries every count.
 *
 * Incomplete by design — it covers exactly the keys that have been migrated.
 * A key with no entry here shows the key itself rather than silently falling
 * back to English, which is what makes the gap visible.
 */
import type { Catalogue } from '../translate';

const id: Catalogue = {
  'nav.dashboard': 'Dasbor',
  'nav.inbox': 'Kotak Masuk Saya',
  'nav.allTasks': 'Semua Tugas',
  'nav.processes': 'Proses',
  'nav.decisions': 'Keputusan',
  'nav.connectors': 'Konektor',
  'nav.sdkSandbox': 'Uji Coba SDK',
  'nav.instances': 'Instansi',
  'nav.projects': 'Proyek',
  'nav.organizations': 'Organisasi',
  'nav.groups': 'Grup',
  'nav.people': 'Orang',
  'nav.platformAccess': 'Akses platform',
  'nav.sectionWork': 'Pekerjaan',
  'nav.sectionBuild': 'Bangun',
  'nav.sectionOperate': 'Operasikan',
  'nav.sectionAdminister': 'Kelola',
  'nav.collapse': 'Ciutkan',
  'nav.mainLabel': 'Navigasi utama',

  'login.welcome': 'Selamat datang kembali',
  'login.subtitle': 'Masuk untuk melanjutkan ke ruang kerja Anda',
  'login.username': 'Nama pengguna',
  'login.password': 'Kata sandi',
  'login.submit': 'Masuk',
  'login.failedTitle': 'Tidak dapat masuk',
  'login.failedHelp':
    'Periksa nama pengguna dan kata sandi Anda, atau minta administrator menyetel ulang.',

  'inbox.title': 'Kotak Masuk Tugas',
  'inbox.subtitle': 'Kelola dan selesaikan tugas untuk {name}.',
  'inbox.assignedToMe': 'Ditugaskan ke Saya',
  'inbox.availableToClaim': 'Tersedia untuk Diambil',
  'inbox.search': 'Cari tugas…',
  'inbox.columnTask': 'Info Tugas',
  'inbox.columnAssignment': 'Penugasan',
  'inbox.columnAbout': 'Tentang apa',
  'inbox.columnTimeline': 'Lini masa',
  'inbox.columnStatus': 'Status',
  'inbox.columnActions': 'Tindakan',
  'inbox.claim': 'Ambil Tugas',
  'inbox.complete': 'Selesaikan',
  'inbox.noDueDate': 'Tanpa tenggat',
  'inbox.reference': 'Referensi {code}',
  'inbox.allCaughtUp': 'Semua sudah selesai',
  // One form for every count: Indonesian does not inflect for number.
  'inbox.nothingWaiting':
    '{count, plural, =0 {Tidak ada yang memerlukan perhatian Anda} other {# tugas menunggu}}',

  'offline.title': 'Anda sedang luring',
  'offline.body':
    'Anda masih dapat membaca yang sudah dimuat, dan menyelesaikan tugas di kotak masuk. Semuanya akan dikirim saat Anda kembali daring.',
  'offline.savedTitle': 'Disimpan di perangkat ini',
  'offline.queued': '{count, plural, other {# perubahan menunggu untuk dikirim}}',
  'offline.sending': 'Sedang dikirim. Disimpan di sini sampai server mengonfirmasi setiap satu.',
  'offline.keptHere': 'Disimpan di perangkat ini sampai Anda kembali daring.',
  'offline.tryAgain': 'Coba lagi sekarang',
  'update.title': 'Versi baru siap',
  'update.body':
    'Muat ulang saat Anda berada di titik berhenti yang tepat. Apa pun yang sedang Anda ketik belum tersimpan.',
  'update.reload': 'Muat ulang sekarang',
  'update.later': 'Nanti',

  'common.cancel': 'Batal',
  'common.save': 'Simpan',
  'common.delete': 'Hapus',
  'common.close': 'Tutup',
  'common.retry': 'Coba lagi',
  'common.loading': 'Memuat…',
  'common.language': 'Bahasa',

  // Page headings. See the English catalogue for why these exist.
  'page.dashboard.title': 'Dasbor',
  'page.dashboard.subtitle': 'Ringkasan proses bisnis dan tugas Anda.',
  'page.welcome.title': 'Selamat datang di Metis BPM',
  'page.welcome.subtitle': 'Mulailah dengan memilih atau membuat proyek.',
  'page.allTasks.title': 'Semua Tugas',
  'page.allTasks.subtitle': 'Setiap tugas dalam proyek ini, milik siapa pun.',
  'page.instances.title': 'Instansi Proses',
  'page.instances.subtitle': 'Setiap jalannya proses dalam proyek ini, dan posisi masing-masing.',
  'page.processes.title': 'Proses',
  'page.processes.subtitle': 'Rancang, terapkan, dan kelola versi model proses yang dijalankan proyek ini.',
  'page.decisions.title': 'Keputusan',
  'page.decisions.subtitle': 'Aturan bisnis sebagai tabel keputusan, berversi dan dapat diuji.',
  'page.decisionTables.title': 'Tabel Keputusan',
  'page.decisionTables.subtitle': 'Kelola tabel keputusan dan aturan bisnis yang kompatibel dengan DMN.',
  'page.definitions.title': 'Proses',
  'page.definitions.subtitle': 'Rancang, terapkan, dan kelola model proses bisnis Anda.',
  'page.connectors.title': 'Konektor',
  'page.connectors.subtitle': 'Layanan yang dipanggil proses Anda, dan yang memanggil proses Anda.',
  'page.people.title': 'Orang',
  'page.people.subtitle': 'Siapa saja yang dapat diberi pekerjaan oleh proses proyek ini. Terpisah dari akun yang mengelola Metis.',
  'page.groups.title': 'Grup',
  'page.groups.subtitle': 'Kelola grup pengguna dan keanggotaannya.',
  'page.projects.title': 'Proyek',
  'page.projects.subtitle': 'Atur proses dan tugas Anda ke dalam proyek.',
  'page.organizations.title': 'Organisasi',
  'page.organizations.subtitle': 'Kelola organisasi Anda beserta proyeknya.',
  'page.platformAccess.title': 'Akses platform',
  'page.platformAccess.subtitle': 'Akun yang mengelola Metis: mereka masuk, mengonfigurasi instalasi, dan menyusun model. Orang yang diberi pekerjaan oleh proses ada di halaman Orang.',
  'page.profile.title': 'Profil Pengguna',
  'page.profile.subtitle': 'Kelola informasi pribadi dan pengaturan akun Anda.',
  'page.settings.title': 'Pengaturan Aplikasi',
  'page.settings.subtitle': 'Konfigurasikan ruang kerja dan preferensi Anda.',

  // The dashboard. See the English catalogue.
  'dash.activeInstances': 'Instansi Aktif',
  'dash.activeInstancesHint': 'Proses yang sedang berjalan',
  'dash.processModels': 'Model Proses',
  'dash.processModelsHint': 'Definisi yang diterapkan dalam proyek ini',
  'dash.tasksCompleted': 'Tugas Selesai',
  'dash.tasksProgress': '{done} dari {total}',
  'dash.needsAttention': 'Perlu Perhatian',
  'dash.needsAttentionSome': 'Instansi tersendat dan menunggu seseorang',
  'dash.needsAttentionNone': 'Tidak ada yang tersendat',
  'dash.timeline': 'Lini Masa Bisnis',
  'dash.recentActivity': 'Aktivitas Terbaru',
  'dash.viewAllInstances': 'Lihat semua instansi',
  'dash.generateReport': 'Buat Laporan',
  'dash.noActivity': 'Belum ada aktivitas',
  'dash.noActivityHint': 'Mulai sebuah proses untuk melihat lini masa aktivitas di sini.',
  'dash.readyTitle': 'Siap mengotomatiskan?',
  'dash.readyLoading': 'Memuat proyek Anda. Jika tetap di sini, pilih satu dari header.',
  'dash.readyNoProjects': 'Proyek mengelompokkan model proses, tugas, dan instansi yang berkaitan. Anda memerlukan satu untuk memulai.',
  'dash.createFirstProject': 'Buat proyek pertama Anda',
  'common.complete': 'Selesai',
  'dash.startFrom': 'Mulai dari templat',
  'dash.startFromHint': 'Proses yang sudah berjalan dan bisa Anda ubah, bukan kanvas kosong',
  'dash.useTemplate': 'Gunakan templat ini',

  // Memulai: kartu di Dasbor dan daftar periksa di Bantuan.
  'start.title': 'Memulai',
  'start.progress': '{done} dari {total} selesai',
  'start.progressLabel': '{done} dari {total} langkah memulai selesai',
  'start.hide': 'Sembunyikan panduan memulai',
  'start.hideHint': 'Kemajuan Anda tetap ada di Bantuan, tanda tanya di bagian atas.',
  'start.next': 'Berikutnya',
  'start.done': 'Selesai:',
  'start.unknownTitle': 'Tidak dapat memeriksa kemajuan proyek ini',
  'start.unknownHint':
    'Belum ada yang ditandai selesai atau belum selesai sampai bisa diperiksa. Mencoba lagi biasanya berhasil.',
  'start.deployProcess.label': 'Terapkan sebuah proses',
  'start.deployProcess.description':
    'Gambar satu di Proses, atau pilih templat di Dasbor, lalu terapkan agar bisa berjalan.',
  'start.startInstance.label': 'Mulai sebuah instansi',
  'start.startInstance.description':
    'Jalankan proses Anda sekali. Setiap jalannya adalah instansi yang bisa Anda ikuti langkah demi langkah.',
  'start.completeTask.label': 'Selesaikan sebuah tugas',
  'start.completeTask.description':
    'Saat proses membutuhkan seseorang, tugasnya menunggu di kotak masuk sampai ada yang menyelesaikannya.',
  'start.connectSystem.label': 'Hubungkan sistem lain',
  'start.connectSystem.description':
    'Siapkan koneksi, misalnya email atau Slack, agar langkah-langkah Anda bisa memanggilnya.',
  'start.addPeople.label': 'Tambahkan orang yang mengerjakannya',
  'start.addPeople.description': 'Impor orang-orang yang bisa diberi tugas oleh proses Anda.',

  // Bantuan
  'help.title': 'Bantuan',
  'help.glossary': 'Glosarium',

  // Glosarium. Nama langkah tetap seperti di palet, yang masih berbahasa
  // Inggris, agar bisa dicari dengan nama yang terlihat di sana.
  'glossary.search': 'Cari di glosarium',
  'glossary.searchPlaceholder': 'Cari, misalnya gateway atau versi aktif',
  'glossary.count': '{count, plural, other {# istilah}}',
  'glossary.matches': '{count} dari {total} cocok',
  'glossary.noMatch': 'Tidak ada yang cocok. Coba kata yang lebih pendek, atau nama lainnya.',
  'glossary.alsoCalled': 'disebut juga {name}',
  'glossary.example': 'Contoh: {example}',
  'glossary.instance.term': 'Instansi',
  'glossary.instance.definition':
    'Satu kali jalannya sebuah proses, dari awal sampai selesai. Masing-masing membawa informasinya sendiri dan berada di langkahnya sendiri.',
  'glossary.instance.example':
    'Setiap klaim biaya yang diajukan seseorang adalah instansi tersendiri dari proses klaim biaya.',
  'glossary.deploy.term': 'Penerapan',
  'glossary.deploy.definition':
    'Menerbitkan sebuah proses agar bisa berjalan. Setiap penerapan menyimpan versi baru, dan instansi yang sudah berjalan dibiarkan apa adanya.',
  'glossary.version.term': 'Versi',
  'glossary.version.definition':
    'Salinan bernomor dari sebuah proses, disimpan setiap kali diterapkan. Sebuah instansi tetap pada versi tempat ia dimulai, kecuali seseorang memindahkannya.',
  'glossary.liveVersion.term': 'Versi aktif',
  'glossary.liveVersion.definition':
    'Versi tempat instansi baru dimulai. Sebuah proses hanya punya satu dalam satu waktu: penerapan biasanya menjadikan versi baru aktif, dan “Version history” bisa menjadikan versi lain aktif, saat itu juga atau pada waktu yang Anda pilih.',
  'glossary.stagedVersion.term': 'Versi siaga',
  'glossary.stagedVersion.definition':
    'Versi yang sudah diterapkan tetapi belum aktif, sehingga instansi baru tetap dimulai pada versi aktif. Di “Version history” Anda bisa menjalankannya untuk mencoba tanpa mengaktifkannya, lalu mengaktifkannya dengan “Make live” atau menjadwalkannya untuk menggantikan versi aktif pada waktu yang Anda pilih.',
  'glossary.incident.term': 'Insiden',
  'glossary.incident.definition':
    'Langkah yang tidak bisa selesai, sehingga instansinya menunggu di sana sampai ada yang menanganinya. Biasanya panggilan ke sistem lain yang terus gagal. Pilihan tanpa jalur yang bisa diambil menimbulkan insiden bila didahului langkah otomatis; bila didahului tugas seseorang, penyelesaian tugas itu ditolak dan tugasnya tetap terbuka. Perbaiki penyebabnya, lalu ulangi langkahnya.',
  'glossary.connection.term': 'Koneksi',
  'glossary.connection.definition':
    'Konektor yang disiapkan untuk satu proyek, dengan alamat dan kredensial milik proyek itu: ruang kerja Slack Anda, bukan Slack secara umum. Langkah yang memakai konektor memanggil lewat koneksi proyeknya.',
  'glossary.connector.term': 'Konektor',
  'glossary.connector.definition':
    'Cara siap pakai untuk memanggil sejenis sistem, seperti Slack, email, basis data, atau API web. Sebuah proyek menyiapkan koneksi ke konektor itu sebelum langkah-langkahnya bisa memakainya.',
  'glossary.decisionTable.term': 'Tabel keputusan',
  'glossary.decisionTable.definition':
    'Aturan yang ditulis sebagai baris-baris tabel: bila masukan cocok dengan sebuah baris, baris itulah yang memberi jawaban. Kebijakannya ada di tabel, jadi bisa diubah tanpa mengubah prosesnya.',
  'glossary.decisionTable.example': 'Klaim di bawah Rp5 juta disetujui otomatis; yang lebih besar diteruskan ke manajer.',
  'glossary.hitPolicy.term': 'Aturan kecocokan',
  'glossary.hitPolicy.definition':
    'Apa yang dilakukan tabel keputusan bila lebih dari satu baris cocok: ambil yang pertama, izinkan hanya satu, kumpulkan semua yang cocok, dan seterusnya.',
  'glossary.hitPolicy.example':
    'Dua baris diskon cocok dengan pesanan yang sama, dan baris pertama yang cocok yang dipakai.',
};

export default id;
