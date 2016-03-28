/*
 * The MIT License
 *
 * Copyright (c) 2013, Jan Molak, SmartCode Ltd http://smartcodeltd.co.uk
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and
 * associated documentation files (the "Software"), to deal in the Software without restriction,
 * including without limitation the rights to use, copy, modify, merge, publish, distribute,
 * sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all copies or
 * substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT
 * NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
 * NON-INFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM,
 * DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */
package com.smartcodeltd.jenkinsci.plugins.buildmonitor;

import static hudson.Util.filter;

import com.smartcodeltd.jenkinsci.plugins.buildmonitor.order.ByName;
import com.smartcodeltd.jenkinsci.plugins.buildmonitor.viewmodel.JobView;
import com.smartcodeltd.jenkinsci.plugins.buildmonitor.viewmodel.plugins.BuildAugmentor;

import net.sf.json.JSONObject;
import net.sf.json.JSONSerializer;

import org.apache.commons.io.IOUtils;
import org.apache.commons.lang3.StringEscapeUtils;
import org.codehaus.jackson.map.ObjectMapper;
import org.kohsuke.stapler.DataBoundConstructor;
import org.kohsuke.stapler.QueryParameter;
import org.kohsuke.stapler.StaplerRequest;
import org.kohsuke.stapler.bind.JavaScriptMethod;

import hudson.Extension;
import hudson.Util;
import hudson.model.AbstractProject;
import hudson.model.ListView;
import hudson.model.ViewDescriptor;
import hudson.model.Descriptor.FormException;
import hudson.util.FormValidation;

import java.io.BufferedReader;
import java.io.FileReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.Random;
import java.util.regex.Pattern;
import java.util.regex.PatternSyntaxException;

import javax.servlet.ServletException;

/**
 * @author Jan Molak
 */
public class BuildMonitorView extends ListView {

  private final static String EXPRESSION_FILE = "/usr/local/google/home/veyron/green_expressions";
  private final static long TITLE_UPDATE_INTERVAL_MS = 300 * 1000;

  private String lastSuccessfulTitle = "";
  private long successfulTitleLastUpdateTime = 0;

  /**
   * @param name Name of the view
   */
  @DataBoundConstructor
  public BuildMonitorView(String name) {
    super(name);
  }

  @Extension
  public static final class Descriptor extends ViewDescriptor {
    public Descriptor() {
      super(BuildMonitorView.class);
    }

    @Override
    public String getDisplayName() {
      return "Vanadium Build Monitor";
    }

    /**
     * Cut-n-paste from ListView$Descriptor as we cannot inherit from that class
     */
    public FormValidation doCheckIncludeRegex(@QueryParameter String value) {
      String v = Util.fixEmpty(value);
      if (v != null) {
        try {
          Pattern.compile(v);
        } catch (PatternSyntaxException pse) {
          return FormValidation.error(pse.getMessage());
        }
      }
      return FormValidation.ok();
    }
  }

  @Override
  protected void submit(StaplerRequest req) throws ServletException, IOException, FormException {
    super.submit(req);

    String requestedOrdering = req.getParameter("order");

    try {
      order = orderIn(requestedOrdering);
    } catch (Exception e) {
      throw new FormException("Can't order projects by " + requestedOrdering, "order");
    }
  }

  // defensive coding to avoid issues when Jenkins instantiates the plugin without populating its
  // fields
  // https://github.com/jan-molak/jenkins-build-monitor-plugin/issues/43
  private Comparator<AbstractProject> currentOrderOrDefault() {
    return order == null ? new ByName() : order;
  }

  public String currentOrder() {
    return currentOrderOrDefault().getClass().getSimpleName();
  }

  private Comparator<AbstractProject> order = new ByName();

  @SuppressWarnings("unchecked")
  private Comparator<AbstractProject> orderIn(String requestedOrdering)
      throws ClassNotFoundException, IllegalAccessException, InstantiationException {
    String packageName = this.getClass().getPackage().getName() + ".order.";

    return (Comparator<AbstractProject>) Class.forName(packageName + requestedOrdering)
        .newInstance();
  }

  /**
   * Because of how org.kohsuke.stapler.HttpResponseRenderer is implemented it can only work with
   * net.sf.JSONObject in order to produce correct application/json output
   *
   * @return
   * @throws Exception
   */
  @JavaScriptMethod
  public JSONObject fetchJobViews() throws Exception {
    return jsonFrom(jobViews());
  }

  public boolean isEmpty() {
    return jobViews().isEmpty();
  }

  private String getDashboardTitle(List<JobView> jobViews) {
    boolean allSuccessful = true;
    String title = "Build Monitor";
    for (JobView jv : jobViews) {
      if (!jv.status().startsWith("successful")) {
        allSuccessful = false;
        break;
      }
    }
    if (allSuccessful) {
      if (System.currentTimeMillis() - successfulTitleLastUpdateTime < TITLE_UPDATE_INTERVAL_MS) {
        title = lastSuccessfulTitle;
      } else {
        // Read expressions file.
        List<String> expressions = new ArrayList<String>();
        BufferedReader br = null;
        try {
          br = new BufferedReader(new FileReader(EXPRESSION_FILE));
          String line;
          while ((line = br.readLine()) != null) {
            if (!line.trim().equals("")) {
              expressions.add(line);
            }
          }
        } catch (Exception e) {
        } finally {
          if (br != null) {
            IOUtils.closeQuietly(br);
          }
        }

        if (expressions.size() != 0) {
          Random rand = new Random();
          int randomIndex = rand.nextInt(expressions.size());
          title = expressions.get(randomIndex);
        }

        successfulTitleLastUpdateTime = System.currentTimeMillis();
        lastSuccessfulTitle = title;
      }
    }
    return title;
  }

  private String getOncall() {
    String postSubmitRoot =
        "/usr/local/google/home/veyron/.jenkins/jobs/vanadium-postsubmit-poll/workspace/root";
    Path path = Paths.get(postSubmitRoot, ".jiri_root/bin/jiri-oncall");
    ProcessBuilder ps = new ProcessBuilder(path.toString());
    Map<String, String> env = ps.environment();
    env.put("JIRI_ROOT", postSubmitRoot);
    ps.redirectErrorStream(true);
    String output = "";
    BufferedReader in = null;
    try {
      Process pr = ps.start();
      in = new BufferedReader(new InputStreamReader(pr.getInputStream()));
      String line;
      while ((line = in.readLine()) != null) {
        output += line;
      }
      pr.waitFor();
    } catch (Exception e) {
    } finally {
      if (in != null) {
        IOUtils.closeQuietly(in);
      }
    }
    if (output.split(" ").length != 1) {
      return "unknown";
    }
    return output == "" ? "unknown" : output;
  }

  private JSONObject jsonFrom(List<JobView> jobViews) throws IOException {
    ObjectMapper m = new ObjectMapper();

    return (JSONObject) JSONSerializer.toJSON("{jobs:" + m.writeValueAsString(jobViews)
        + ", 'title':'" + StringEscapeUtils.escapeEcmaScript(getDashboardTitle(jobViews))
        + "', 'oncall':'" + getOncall() + "'}");
  }

  private List<JobView> jobViews() {
    List<AbstractProject> projects = filter(super.getItems(), AbstractProject.class);
    List<JobView> jobs = new ArrayList<JobView>();

    Collections.sort(projects, currentOrderOrDefault());

    for (AbstractProject project : projects) {
      JobView jv = JobView.of(project, withAugmentationsIfTheyArePresent());
      jobs.add(jv);
    }

    return jobs;
  }

  private BuildAugmentor withAugmentationsIfTheyArePresent() {
    return BuildAugmentor.fromDetectedPlugins();
  }
}
