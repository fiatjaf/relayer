resource "google_service_account" "service-account" {
  project      = var.project_id
  account_id   = "nostr-relayer"
  display_name = "nostr-relayer"
}

resource "google_project_iam_member" "nostr-relayer" {
  project = var.project_id
  role    = "roles/editor"
  member  = "serviceAccount:${google_service_account.service-account.email}"
}

resource "google_compute_firewall" "firewall" {
  name    = "nostr-relayer-firewall"
  project = var.project_id
  network = "default"

  allow {
    protocol = "icmp"
  }

  allow {
    protocol = "tcp"
    ports    = ["22", "80", "443", "2700"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["nostr-relayer"]
}

resource "google_compute_address" "static" {
  name       = "vm-public-address"
  project    = var.project_id
  region     = var.region
  depends_on = [google_compute_firewall.firewall]
}

resource "google_compute_instance" "nostr-relayer" {
  name         = "nostr-relayer"
  zone         = "${var.region}-b"
  machine_type = var.machine_type
  tags         = ["nostr-relayer"]

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
    }
  }

  network_interface {
    network = "default"
    access_config {
      nat_ip = google_compute_address.static.address
    }
  }

  labels = {
    name = "nostr-relayer"
  }

  connection {
    type        = "ssh"
    user        = "ubuntu"
    timeout     = "1000s"
    private_key = file(var.private_keypath)
    host        = google_compute_address.static.address
  }

  provisioner "local-exec" {
    command = <<EOT
    sleep 20 && \
    > hosts && \
    echo "[relayer]" | tee -a hosts && \
    echo "${google_compute_address.static.address} ansible_user=ubuntu ansible_ssh_private_key_file=${var.private_keypath}" | tee -a hosts && \
    export ANSIBLE_HOST_KEY_CHECKING=False && \
    echo "${google_compute_address.static.address} ansible_user=ubuntu ansible_ssh_private_key_file=${var.private_keypath}" | tee -a hosts && \
    export ANSIBLE_HOST_KEY_CHECKING=False && \
    ansible-galaxy install -r ../ansible/requirements.yml
    ansible-playbook -u ubuntu --private-key ${var.private_keypath} -i hosts site.yml
  EOT

  }

  depends_on = [google_compute_firewall.firewall]

  metadata = {
    ssh-keys = "ubuntu:${file(var.public_keypath)}"
  }
}